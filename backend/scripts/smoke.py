#!/usr/bin/env python3
import argparse
from datetime import datetime, timedelta
from decimal import Decimal
import json
from pathlib import Path
import re
import struct
import time
from urllib.error import HTTPError
from urllib.parse import quote
from urllib.request import Request, urlopen
import uuid
from zoneinfo import ZoneInfo
import zlib


class Smoke:
    def __init__(self, api, mailpit):
        self.api, self.mailpit = api.rstrip('/'), mailpit.rstrip('/')
        self.checks = []

    def call(self, method, path, body=None, token='', status=200, match=None, key=None):
        headers = {'X-App-Version': '1.0.0', 'Idempotency-Key': key or str(uuid.uuid4())}
        if token:
            headers['Authorization'] = 'Bearer ' + token
        if match is not None:
            headers['If-Match'] = str(match)
        data = None
        if body is not None:
            data = json.dumps(body, ensure_ascii=False).encode()
            headers['Content-Type'] = 'application/json'
        try:
            response = urlopen(Request(self.api + path, data, headers, method=method), timeout=20)
        except HTTPError as error:
            response = error
        raw = response.read()
        if response.code != status:
            raise AssertionError(f'{method} {path}: status {response.code}, expected {status}')
        self.checks.append({'method': method, 'status': response.code})
        return json.loads(raw).get('data')

    def user(self):
        email = 'smoke-' + uuid.uuid4().hex + '@example.test'
        self.call('POST', '/it4788/v1/auth/register', {'fullName': 'Nguyễn An', 'email': email, 'password': 'SmokePassword123', 'acceptTerms': True}, status=201)
        deadline, code = time.monotonic() + 20, None
        while time.monotonic() < deadline:
            messages = json.load(urlopen(self.mailpit + '/api/v1/search?query=' + quote('to:' + email), timeout=5))['messages']
            if messages:
                message = json.load(urlopen(self.mailpit + '/api/v1/message/' + messages[0]['ID'], timeout=5))
                found = re.search(r'\b[0-9]{6}\b', message['Text'])
                if found:
                    code = found.group()
                    break
            time.sleep(0.1)
        if code is None:
            raise AssertionError('OTP not delivered through SMTP and worker')
        return self.call('POST', '/it4788/v1/auth/otp/verify', {'email': email, 'code': code, 'purpose': 'register'})

    def run(self):
        self.call('GET', '/healthz')
        self.call('GET', '/readyz')
        owner, member = self.user(), self.user()
        token, other = owner['accessToken'], member['accessToken']
        group = self.call('POST', '/it4788/v1/spaces', {'name': 'Gia đình kiểm thử'}, token, 201)
        base = '/it4788/v1/spaces/' + group['id']
        invite = self.call('POST', base + '/invites', {}, token, 201)
        self.call('POST', '/it4788/v1/spaces/join', {'code': invite['code'], 'dryRun': True}, other)
        self.call('POST', '/it4788/v1/spaces/join', {'code': invite['code']}, other)
        self.call('GET', base, token=other)
        today = datetime.now(ZoneInfo('Asia/Ho_Chi_Minh')).date()
        tomorrow, expiry = today + timedelta(days=1), today + timedelta(days=3)
        food, unit = '00000000-0000-7000-8000-000000000021', '00000000-0000-7000-8000-000000000012'
        shopping = self.call('POST', base + '/shopping-lists', {'name': 'Đi chợ', 'plannedDate': str(today)}, token, 201)
        item = self.call('POST', base + '/shopping-lists/' + shopping['id'] + '/items', {'foodId': food, 'unitId': unit, 'quantity': '1'}, token, 201)
        self.call('POST', base + '/shopping-items/purchase', {'items': [{'itemId': item['id'], 'unitPrice': 20000}]}, other)
        stocked = self.call('POST', base + '/shopping-items/stock', {'items': [{'itemId': item['id'], 'storageLocation': 'chill', 'expiresOn': str(expiry)}]}, token)
        lot = stocked['results'][0]['lotId']
        recipe = self.call('POST', base + '/recipes', {'name': 'Canh cà rốt', 'servings': 1, 'ingredients': [{'foodId': food, 'unitId': unit, 'quantity': '0.2'}]}, token, 201)
        meals = []
        for day, slot in [(today, 'lunch'), (tomorrow, 'dinner')]:
            meals.append(self.call('POST', base + '/meals', {'recipeId': recipe['id'], 'date': str(day), 'mealSlot': slot, 'servings': 3}, token, 201))
        preview = self.call('POST', base + '/shopping-lists/generate', {'from': str(today), 'to': str(tomorrow), 'dryRun': True}, token)
        assert len(preview['lines']) == 1
        assert Decimal(str(preview['lines'][0]['shortQty'])) == Decimal('0.2')
        self.call('POST', base + '/shopping-lists/generate', {'from': str(today), 'to': str(tomorrow), 'plannedDate': str(today), 'name': 'Bù nguyên liệu', 'lines': [{'foodId': food, 'unitId': unit, 'quantity': '0.2'}]}, token, 201)
        self.call('GET', base + '/recipe-suggestions?servings=3&limit=3', token=other)
        self.call('POST', base + '/meals/' + meals[0]['id'] + '/cook', {}, token, match=meals[0]['version'])
        self.call('POST', base + '/meals/' + meals[0]['id'] + '/cook', {}, token, match=meals[0]['version'])
        current = self.call('GET', base + '/lots/' + lot, token=token)
        assert Decimal(str(current['remainingQuantity'])) == Decimal('0.4')
        self.call('POST', base + '/lots/' + lot + '/discard', {'quantity': '0.1', 'reason': 'spoiled'}, token)
        totals = {}
        for kind, expected in [('spending', 20000), ('consumption', 12000), ('waste', 2000)]:
            report = self.call('GET', base + '/reports/' + kind + '?from=' + str(today) + '&to=' + str(today), token=token)
            assert report['totalCost'] == expected, (kind, report['totalCost'])
            totals[kind] = report['totalCost']
        for kind in ['csv', 'pdf']:
            job = self.call('POST', base + '/reports/exports', {'reportType': 'consumption', 'from': str(today), 'to': str(today), 'format': kind}, token, 202)
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                result = self.call('GET', base + '/reports/exports/' + job['exportId'], token=token)
                if result['status'] == 'ready':
                    content = urlopen(result['downloadUrl'], timeout=10).read()
                    assert content.startswith(b'%PDF') if kind == 'pdf' else 'Cà rốt' in content.decode('utf-8-sig')
                    break
                time.sleep(0.2)
            else:
                raise AssertionError('export worker did not publish ' + kind)
        chunk = lambda kind, data: struct.pack('!I', len(data)) + kind + data + struct.pack('!I', zlib.crc32(kind + data) & 0xffffffff)
        png = b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('!IIBBBBB', 1, 1, 8, 2, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(b'\x00\xff\x00\x00')) + chunk(b'IEND', b'')
        grant = self.call('POST', '/it4788/v1/media/upload-urls', {'purpose': 'avatar', 'targetId': owner['userId'], 'contentType': 'image/png', 'size': len(png)}, token, 201)
        uploaded = urlopen(Request(grant['uploadUrl'], png, grant['headers'], method='PUT'), timeout=10)
        assert uploaded.code == 200
        self.call('POST', '/it4788/v1/media/' + grant['objectId'] + '/complete', {}, token)
        profile = self.call('GET', '/it4788/v1/me', token=token)
        self.call('PATCH', '/it4788/v1/me', {'avatarObjectId': grant['objectId']}, token, match=profile['version'])
        photo = self.call('GET', '/it4788/v1/media/' + grant['objectId'], token=token)
        assert urlopen(photo['downloadUrl'], timeout=10).read() == png
        return {'scenarios': ['K1', 'K2', 'K3', 'K4', 'K5'], 'checks': self.checks, 'reportTotalsVND': totals, 'smtp': 'real Mailpit', 's3': 'real signed upload/download', 'exports': ['csv', 'pdf']}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--api', default='http://127.0.0.1:58080')
    parser.add_argument('--mailpit', default='http://127.0.0.1:58025')
    parser.add_argument('--report', default='../output/backend-qa/network-smoke.json')
    args = parser.parse_args()
    result = Smoke(args.api, args.mailpit).run()
    path = Path(args.report)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
    print(f'K1–K5 PASS: {len(result["checks"])} HTTP checks; SMTP, S3, CSV/PDF; evidence: {path}')


if __name__ == '__main__':
    main()
