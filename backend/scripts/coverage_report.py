#!/usr/bin/env python3
"""Check combined Go statement coverage and retain every package in the report."""

import argparse
from collections import Counter
import json
from pathlib import Path
import re
import subprocess
import sys

PACKAGE_MINIMUMS = {
    'dicho/internal/app': 85,
    'dicho/internal/platform': 85,
    'dicho/internal/kernel': 90,
    **{'dicho/internal/modules/' + name: 85 for name in
       ['accounts', 'exports', 'inventory', 'media', 'notification', 'planning']},
}
CORE_FILES = {
    'Allocate': 'dicho/internal/modules/inventory/domain.go',
    'Missing': 'dicho/internal/modules/planning/algorithms.go',
    'Score': 'dicho/internal/modules/planning/algorithms.go',
}


def parse_profile(text):
    lines = text.splitlines()
    if not lines or lines[0] not in ['mode: atomic', 'mode: count', 'mode: set']:
        raise ValueError('invalid coverage mode')
    blocks = {}
    for line in lines[1:]:
        if not line.strip():
            continue
        try:
            location, statements, count = line.rsplit(' ', 2)
            statements, count = int(statements), int(count)
        except ValueError:
            raise ValueError('invalid coverage block') from None
        if not re.fullmatch(r'.+:\d+\.\d+,\d+\.\d+', location) or min(statements, count) < 0:
            raise ValueError('invalid coverage block')
        if location in blocks and blocks[location][0] != statements:
            raise ValueError('conflicting statement counts')
        blocks[location] = (statements, max(count, blocks.get(location, (0, 0))[1]))
    if not blocks:
        raise ValueError('empty coverage profile')
    return blocks


def summarize(blocks):
    packages = {}
    for location, (statements, count) in blocks.items():
        package = location.rsplit('/', 1)[0]
        entry = packages.setdefault(package, {'covered': 0, 'statements': 0})
        entry['statements'] += statements
        entry['covered'] += statements if count > 0 else 0
    total = {key: sum(p[key] for p in packages.values()) for key in ['covered', 'statements']}
    for entry in [total, *packages.values()]:
        entry['percent'] = entry['covered'] * 100 / entry['statements'] if entry['statements'] else 0
    return {'total': total, 'packages': dict(sorted(packages.items()))}


def parse_core_functions(text):
    found = {}
    for line in text.splitlines():
        match = re.fullmatch(r'(\S+):\d+:\s+(\S+)\s+([0-9.]+)%', line.strip())
        if match and CORE_FILES.get(match[2]) == match[1]:
            percent = float(match[3])
            if not 0 <= percent <= 100:
                raise ValueError('invalid function coverage')
            found[match[2]] = percent
    return found


def test_results(text):
    counts = Counter()
    packages_failed = []
    for line in text.splitlines():
        if not line.strip():
            continue
        event = json.loads(line)
        action = event.get('Action')
        if 'Test' in event and action in ['pass', 'fail', 'skip']:
            counts[action] += 1
        elif action == 'fail':
            packages_failed.append(event.get('Package', 'unknown'))
    return {key: counts[key] for key in ['pass', 'fail', 'skip']} | {'packagesFailed': packages_failed}


def enforce(summary, core, tests=None):
    failures = []
    if summary['total']['percent'] < 85:
        failures.append('global coverage below85%')
    for name, minimum in PACKAGE_MINIMUMS.items():
        if name not in summary['packages'] or summary['packages'][name]['percent'] < minimum:
            failures.append(name + ' coverage below' + str(minimum) + '% or missing')
    for name in CORE_FILES:
        if name not in core or core[name] < 90:
            failures.append(name + ' coverage below90% or missing')
    if tests is not None and (tests['pass'] == 0 or tests['fail'] or tests['skip'] or tests['packagesFailed']):
        failures.append('tests failed, skipped, missing or package build failed')
    return failures


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('profile')
    parser.add_argument('--tests')
    parser.add_argument('--report', default='../output/backend-qa/coverage-summary.json')
    args = parser.parse_args()
    try:
        summary = summarize(parse_profile(Path(args.profile).read_text()))
        process = subprocess.run(['go', 'tool', 'cover', '-func=' + args.profile], capture_output=True, text=True, check=True)
        core = parse_core_functions(process.stdout)
        tests = test_results(Path(args.tests).read_text()) if args.tests else None
        failures = enforce(summary, core, tests)
        summary.update(coreFunctions=core, tests=tests, failures=failures, status='FAIL' if failures else 'PASS',
                       method='coverpkg=./...; unique statement blocks; global denominator retains all packages',
                       thresholds={'global': 85, 'packages': PACKAGE_MINIMUMS, 'coreFunctions': 90})
        destination = Path(args.report)
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
        print('TOTAL_COVERAGE: {:.2f}%'.format(summary['total']['percent']))
        for name in PACKAGE_MINIMUMS:
            print(name + ': {:.2f}%'.format(summary['packages'].get(name, {}).get('percent', 0)))
        for failure in failures:
            print('FAIL: ' + failure)
        return 1 if failures else 0
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print('Coverage report failed: ' + str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
