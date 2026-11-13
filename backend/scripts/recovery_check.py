#!/usr/bin/env python3
"""Recover a live synthetic PostgreSQL16 fixture without stopping its source."""

import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import shlex
import stat
import subprocess
import tempfile
import time
from urllib.parse import urlsplit, urlunsplit
import uuid

ROOT = Path(__file__).resolve().parents[2]
RESTORE_NAME = re.compile(r'dicho_restore_[0-9]{14}_[0-9a-f]{12}')
RELATIONS_SQL = """SELECT coalesce(jsonb_agg(jsonb_build_array(n.nspname,c.relname)
 ORDER BY n.nspname,c.relname),'[]'::jsonb) FROM pg_class c
 JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname NOT IN ('pg_catalog','information_schema')
 AND n.nspname NOT LIKE 'pg_toast%' AND
 (c.relkind IN ('r','p','m') OR (n.nspname='report' AND c.relkind='v'));"""

AMOUNTS_SQL = """jsonb_build_object(
 'spending_total_vnd',(SELECT coalesce(sum(round(actual_quantity*unit_price)),0)::text
 FROM report.v_spending),
 'lots_by_dimension',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY dimension),'[]') FROM
 (SELECT dimension,sum(initial_qty*unit_factor)::text initial_base,
 sum(base_remaining)::text remaining_base,sum(coalesce(total_cost,0))::text total_cost_vnd
 FROM pantry.food_lots GROUP BY dimension) x),
 'movements_by_kind',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY kind,dimension),'[]') FROM
 (SELECT kind,dimension,sum(quantity)::text quantity_base,
 coalesce(sum(round(total_cost*abs(quantity)/(initial_qty*unit_factor))),0)::text valued_vnd
 FROM report.v_movements GROUP BY kind,dimension) x))"""

INTEGRITY_SQL = """jsonb_build_object(
 'foreign_keys',(SELECT count(*) FROM pg_constraint WHERE contype='f'),
 'unvalidated_foreign_keys',(SELECT count(*) FROM pg_constraint WHERE contype='f' AND NOT convalidated),
 'cross_space_shopping',(SELECT count(*) FROM shopping.shopping_items i
 LEFT JOIN shopping.shopping_lists l ON l.id=i.list_id WHERE l.id IS NULL OR l.space_id<>i.space_id),
 'cross_space_movements',(SELECT count(*) FROM pantry.stock_movements m
 LEFT JOIN pantry.food_lots l ON l.id=m.lot_id WHERE l.id IS NULL OR l.space_id<>m.space_id OR l.food_id<>m.food_id),
 'invalid_lot_balance',(SELECT count(*) FROM pantry.food_lots
 WHERE remaining_qty<0 OR base_remaining<>remaining_qty*unit_factor),
 'movement_balance_mismatch',(SELECT count(*) FROM pantry.food_lots l WHERE l.base_remaining<>
 coalesce((SELECT sum(CASE WHEN m.kind IN ('consume','discard') THEN -m.quantity ELSE m.quantity END)
 FROM pantry.stock_movements m WHERE m.lot_id=l.id),0)),
 'missing_meal_recipe',(SELECT count(*) FROM mealplan.meal_entries m WHERE m.recipe_id IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM recipe.recipes r WHERE r.id=m.recipe_id)),
 'missing_recipe_image',(SELECT count(*) FROM recipe.recipes r WHERE r.image_object_id IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM media.objects o WHERE o.id=r.image_object_id AND o.status='ready')),
 'missing_user_avatar',(SELECT count(*) FROM identity.users u WHERE u.avatar_object_id IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM media.objects o WHERE o.id=u.avatar_object_id AND o.status='ready')))"""


class RecoveryError(Exception):
    pass


def identifier(value):
    return '"' + value.replace('"', '""') + '"'


def literal(value):
    return "'" + value.replace("'", "''") + "'"


class Recovery:
    def __init__(self, args):
        self.args, self.commands = args, []
        self.created = False
        self.target = 'dicho_restore_' + datetime.now(timezone.utc).strftime('%Y%m%d%H%M%S') + '_' + uuid.uuid4().hex[:12]
        self.result = {'status': 'FAIL', 'source': {'container': args.postgres_container, 'database': args.source_db},
                       'restoreDatabase': self.target, 'commands': self.commands,
                       'limits': ['Synthetic local fixture; no production RPO/RTO or seven-backup retention claim.',
                                  'Database archive excludes external S3/SMTP/Redis state and filesystem signing/queue keys.',
                                  'Worker trial uses a new isolated recovery queue and synthetic space.copy job; pre-existing encrypted job recovery is not verified.']}
        self.phase = 'initialization'

    def run_command(self, argv, label, *, input_data=None, stdin=None, stdout=None, env=None, cwd=None, timeout=120):
        self.phase = label
        started = time.monotonic()
        record = {'command': shlex.join([str(v) for v in argv]), 'purpose': label}
        if cwd:
            record['workingDirectory'] = str(cwd)
        self.commands.append(record)
        try:
            process = subprocess.run(argv, input=input_data, stdin=stdin, stdout=stdout or subprocess.PIPE,
                                     stderr=subprocess.PIPE, env=env, cwd=cwd, timeout=timeout)
        except (subprocess.TimeoutExpired, OSError) as error:
            record.update(exitCode=None, durationSeconds=round(time.monotonic()-started, 6))
            raise RecoveryError(label + ': execution failed (' + type(error).__name__ + ')') from None
        record.update(exitCode=process.returncode, durationSeconds=round(time.monotonic()-started, 6))
        if process.returncode:
            if label == 'real isolated River space.copy retry on restored database':
                diagnostic = re.search(rb'recovery worker trial failed during ([A-Za-z ._-]+)', process.stderr)
                if diagnostic:
                    self.result['workerFailurePhase'] = diagnostic.group(1).decode().strip()
            raise RecoveryError(label + ': command failed; provider output omitted to protect credentials')
        return process.stdout

    def psql_args(self, database):
        return ['docker', 'exec', '-i', self.args.postgres_container, 'psql', '-X', '-qAt',
                '-v', 'ON_ERROR_STOP=1', '-U', self.args.user, '-d', database]

    def sql(self, database, query, label, snapshot=None):
        prefix = 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\n'
        if snapshot:
            prefix += 'SET TRANSACTION SNAPSHOT ' + literal(snapshot) + ';\n'
        prefix += "SET LOCAL TIME ZONE 'UTC'; SET LOCAL extra_float_digits=3; SET LOCAL statement_timeout='60s';\n"
        raw = self.run_command(self.psql_args(database), label, input_data=(prefix+query+'\nROLLBACK;\n').encode())
        try:
            return json.loads(raw)
        except (json.JSONDecodeError, UnicodeDecodeError):
            raise RecoveryError(label + ': invalid structured SQL output') from None

    @contextmanager
    def exported_snapshot(self):
        self.phase = 'export snapshot'
        argv = self.psql_args(self.args.source_db)
        process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        started = time.monotonic()
        try:
            process.stdin.write(b'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\nSELECT pg_export_snapshot();\n')
            process.stdin.flush()
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                if not selector.select(15):
                    raise RecoveryError('export snapshot: timeout')
                snapshot = process.stdout.readline().decode().strip()
            if not re.fullmatch(r'[0-9A-Fa-f]+-[0-9A-Fa-f]+-[0-9]+', snapshot):
                raise RecoveryError('export snapshot: invalid snapshot identifier')
            self.result['snapshot'] = {'id': snapshot, 'isolation': 'REPEATABLE READ READ ONLY',
                                       'exportedAtUTC': datetime.now(timezone.utc).isoformat()}
            yield snapshot
        finally:
            if process.poll() is None:
                try:
                    process.stdin.write(b'ROLLBACK;\n\\q\n')
                    process.stdin.flush()
                    process.wait(timeout=10)
                except (BrokenPipeError, subprocess.TimeoutExpired):
                    process.kill()
                    process.wait()
            for stream in [process.stdin, process.stdout, process.stderr]:
                stream.close()
            self.commands.append({'command': shlex.join(argv), 'purpose': 'exported snapshot keeper (read-only)',
                                  'exitCode': process.returncode, 'durationSeconds': round(time.monotonic()-started, 6)})

    def fingerprint(self, database, relations, snapshot=None):
        parts = []
        for schema, table in relations:
            relation = schema + '.' + table
            parts.append("SELECT " + literal(relation) + " relation,count(*) rows,"
                         "encode(sha256(convert_to(coalesce(string_agg(row_hash,'' ORDER BY row_hash),''),'UTF8')),'hex') sha256 "
                         "FROM (SELECT encode(sha256(convert_to(to_jsonb(r)::text,'UTF8')),'hex') row_hash FROM "
                         + identifier(schema) + '.' + identifier(table) + ' r) hashes')
        query = "WITH fingerprints AS (" + ' UNION ALL '.join(parts) + ") SELECT jsonb_build_object(" \
                "'relations',(SELECT jsonb_object_agg(relation,jsonb_build_object('rows',rows,'sha256',sha256)) FROM fingerprints)," \
                "'amounts'," + AMOUNTS_SQL + ",'integrity'," + INTEGRITY_SQL + ');'
        return self.sql(database, query, 'fingerprint '+('source snapshot' if snapshot else database), snapshot)

    def app_state(self, label):
        raw = self.run_command(['docker', 'inspect', '--format', '{{json .State}}', self.args.app_container], label)
        state = json.loads(raw)
        return {'running': state['Running'], 'health': state.get('Health', {}).get('Status', 'unknown')}

    def cleanup(self):
        if self.created:
            if not RESTORE_NAME.fullmatch(self.target) or self.target == self.args.source_db:
                raise RecoveryError('cleanup refused unsafe database target')
            self.run_command(['docker', 'exec', self.args.postgres_container, 'dropdb', '-U', self.args.user,
                              '--force', self.target], 'drop only the created restore database')
            self.created = False
            exists = self.sql('postgres', 'SELECT to_jsonb(EXISTS(SELECT 1 FROM pg_database WHERE datname='+literal(self.target)+'));', 'verify restore database removed')
            if exists:
                raise RecoveryError('temporary restore database still exists')
            self.result['cleanup'] = {'temporaryDatabaseRemoved': True, 'sourceDatabaseUntouched': True}

    def execute(self):
        started = time.monotonic()
        self.result['startedAtUTC'] = datetime.now(timezone.utc).isoformat()
        self.result['scriptSHA256'] = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        self.result['checkoutRevision'] = self.run_command(['git', 'rev-parse', 'HEAD'], 'record current checkout revision', cwd=ROOT).decode().strip()
        self.result['sourceAppBefore'] = self.app_state('source app before recovery')
        if not self.result['sourceAppBefore']['running']:
            raise RecoveryError('source app must remain running for this recovery check')
        version = self.sql(self.args.source_db, "SELECT current_setting('server_version_num')::integer;", 'PostgreSQL server major')
        if version//10000 != 16:
            raise RecoveryError('this check requires PostgreSQL16')
        self.result['postgresVersionNum'] = version
        self.result['pgDumpVersion'] = self.run_command(['docker', 'exec', self.args.postgres_container, 'pg_dump', '--version'], 'pg_dump version').decode().strip()
        archive = Path(self.args.report).resolve().parent / (self.target+'.dump')
        archive.parent.mkdir(parents=True, exist_ok=True)
        with self.exported_snapshot() as snapshot:
            relations = self.sql(self.args.source_db, RELATIONS_SQL, 'discover snapshot relations', snapshot)
            baseline = self.fingerprint(self.args.source_db, relations, snapshot)
            required = ['household.spaces', 'shopping.shopping_items', 'pantry.food_lots', 'pantry.stock_movements',
                        'recipe.recipes', 'mealplan.meal_entries', 'report.exports', 'report.v_spending', 'report.v_movements']
            if any(baseline['relations'].get(name, {}).get('rows', 0) == 0 for name in required):
                raise RecoveryError('synthetic source fixture is incomplete')
            fd = os.open(archive, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, 'wb') as output:
                self.run_command(['docker', 'exec', self.args.postgres_container, 'pg_dump', '-U', self.args.user,
                                  '-d', self.args.source_db, '--format=custom', '--no-owner', '--no-acl',
                                  '--lock-wait-timeout=5s', '--snapshot='+snapshot], 'consistent pg_dump', stdout=output)
        digest = hashlib.sha256()
        with archive.open('rb') as source:
            for chunk in iter(lambda: source.read(1024*1024), b''):
                digest.update(chunk)
        mode = stat.S_IMODE(archive.stat().st_mode)
        if mode != 0o600:
            raise RecoveryError('archive permissions are not0600')
        self.result['archive'] = {'path': str(archive), 'bytes': archive.stat().st_size, 'mode': '0600', 'sha256': digest.hexdigest()}
        with archive.open('rb') as source:
            toc = self.run_command(['docker', 'exec', '-i', self.args.postgres_container, 'pg_restore', '--list'], 'validate custom archive TOC', stdin=source)
        self.result['archive']['tocEntries'] = sum(1 for line in toc.decode().splitlines() if line and not line.startswith(';'))
        if self.sql('postgres', 'SELECT to_jsonb(EXISTS(SELECT 1 FROM pg_database WHERE datname='+literal(self.target)+'));', 'refuse existing restore target'):
            raise RecoveryError('generated restore target already exists; refusing to overwrite')
        self.run_command(['docker', 'exec', self.args.postgres_container, 'createdb', '-U', self.args.user,
                          '--template=template0', self.target], 'create fresh restore database')
        self.created = True
        with archive.open('rb') as source:
            self.run_command(['docker', 'exec', '-i', self.args.postgres_container, 'pg_restore', '-U', self.args.user,
                              '-d', self.target, '--no-owner', '--no-acl', '--exit-on-error', '--single-transaction'],
                             'transactional pg_restore', stdin=source)
        restored = self.fingerprint(self.target, relations)
        self.result['sourceSnapshot'], self.result['restoredBeforeWorker'] = baseline, restored
        if baseline != restored:
            raise RecoveryError('restored counts/fingerprints/amounts/integrity differ from the exported snapshot')
        if any(value for name, value in restored['integrity'].items() if name != 'foreign_keys'):
            raise RecoveryError('restored application integrity check failed')
        self.result['restoreComparison'] = {'status': 'PASS', 'relationsCompared': len(relations),
                                             'allCountsAndSHA256Match': True, 'amountsMatch': True, 'integrityPass': True}
        self.result['workerRetry'] = self.worker_retry() if not self.args.skip_worker_retry else {'status': 'NOT_RUN', 'reason': '--skip-worker-retry selected'}
        self.result['workerRetry']['codeSource'] = 'current checkout production Dispatcher, not the running QA image'
        after = self.fingerprint(self.args.source_db, relations)
        changed = [name for name, value in baseline['relations'].items() if after['relations'][name] != value]
        self.result['sourceAfter'] = {'changedRelationsSinceSnapshot': changed,
                                       'comparisonUsesExportedSnapshot': True}
        self.result['sourceAppAfter'] = self.app_state('source app after recovery')
        if not self.result['sourceAppAfter']['running']:
            raise RecoveryError('source app stopped during recovery check')
        self.result['validationDurationSeconds'] = round(time.monotonic()-started, 6)
        self.result['status'] = 'PASS'

    def worker_retry(self):
        raw = self.run_command(['docker', 'exec', self.args.app_container, 'sh', '-c', 'printf "%s" "$DATABASE_URL"'],
                               'read only database connection environment into memory (output never logged)')
        dsn = urlsplit(raw.decode())
        if dsn.scheme not in ('postgres', 'postgresql') or not dsn.username:
            raise RecoveryError('worker trial cannot obtain database connection configuration')
        published = self.run_command(['docker', 'port', self.args.postgres_container, '5432'], 'locate loopback PostgreSQL port').decode().splitlines()[0]
        host, port = published.rsplit(':', 1)
        if host not in ('127.0.0.1', '0.0.0.0'):
            raise RecoveryError('worker trial requires a loopback PostgreSQL port')
        credentials = dsn.netloc.rsplit('@', 1)[0]
        restored_dsn = urlunsplit((dsn.scheme, credentials+'@127.0.0.1:'+port, '/'+self.target, 'sslmode=disable', ''))
        env = {**os.environ, 'RECOVERY_DATABASE_URL': restored_dsn, 'RECOVERY_DATABASE_NAME': self.target}
        with tempfile.TemporaryDirectory(prefix='.recovery-worker-', dir=ROOT/'backend') as directory:
            helper = Path(directory)/'main.go'
            helper.write_text(WORKER_GO)
            raw = self.run_command(['go', 'run', str(helper)], 'real isolated River space.copy retry on restored database', env=env, cwd=ROOT/'backend', timeout=180)
        try:
            result = json.loads(raw)
        except json.JSONDecodeError:
            raise RecoveryError('worker trial did not return structured evidence') from None
        if result.get('status') != 'PASS':
            raise RecoveryError('worker retry trial failed')
        return result


WORKER_GO = r'''package main

import (
	"context"
	"dicho/internal/app"
	"dicho/internal/modules/accounts"
	"dicho/internal/modules/inventory"
	"dicho/internal/modules/planning"
	"dicho/internal/platform"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

type copyWorker struct {
	river.WorkerDefaults[platform.Event]
	dispatcher *app.Dispatcher
}

func (w *copyWorker) Work(ctx context.Context, j *river.Job[platform.Event]) error {
	if j.Args.Event != "space.copy" {
		return errors.New("isolated recovery queue refuses other events")
	}
	return w.dispatcher.Handle(ctx, j.Args)
}

var stage = "connect"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "recovery worker trial failed during "+stage)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	name := os.Getenv("RECOVERY_DATABASE_NAME")
	if !strings.HasPrefix(name, "dicho_restore_") {
		return errors.New("unsafe database")
	}
	pool, err := pgxpool.New(ctx, os.Getenv("RECOVERY_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	var actual string
	if err = pool.QueryRow(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		return errors.New("wrong database")
	}
	db := platform.NewDB(pool)
	a := accounts.New(db, nil)
	inv := inventory.New(db, a)
	p := planning.New(db, inv, inv, a)
	d := &app.Dispatcher{DB: db, Accounts: a, Inventory: inv, Planning: p}
	user, source, target, list, item, lot, recipe := platform.ID(), platform.ID(), platform.ID(), platform.ID(), platform.ID(), platform.ID(), platform.ID()
	stage = "synthetic fixtures in restored database"
	statements := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Recovery fixture','unused','active')", []any{user, user + "@recovery.test"}},
		{"INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'personal','Recovery source',$3),($2,'family','Recovery target',$3)", []any{source, target, user}},
		{"INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$3,'leader','personal'),($2,$3,'leader','family')", []any{source, target, user}},
		{"INSERT INTO shopping.shopping_lists(id,space_id,name,planned_date) VALUES($1,$2,'Recovery list',current_date)", []any{list, source}},
		{"INSERT INTO shopping.shopping_items(id,space_id,list_id,food_id,name,unit_id,unit_factor,dimension,quantity) VALUES($1,$2,$3,'00000000-0000-7000-8000-000000000021','Cà rốt','00000000-0000-7000-8000-000000000011',1,'mass',2)", []any{item, source, list}},
		{"INSERT INTO pantry.food_lots(id,space_id,food_id,unit_id,unit_factor,dimension,initial_qty,remaining_qty,base_remaining,purchased_on,expires_on,storage_location,total_cost) VALUES($1,$2,'00000000-0000-7000-8000-000000000021','00000000-0000-7000-8000-000000000011',1,'mass',500,500,500,current_date,current_date+3,'chill',10000)", []any{lot, source}},
		{"INSERT INTO pantry.stock_movements(id,space_id,lot_id,food_id,kind,quantity) VALUES($1,$2,$3,'00000000-0000-7000-8000-000000000021','stock_in',500)", []any{platform.ID(), source, lot}},
		{"INSERT INTO recipe.recipes(id,space_id,created_by,name,servings) VALUES($1,$2,$3,'Canh phục hồi',2)", []any{recipe, source, user}},
		{"INSERT INTO recipe.recipe_ingredients(id,recipe_id,food_id,unit_id,unit_factor,dimension,quantity) VALUES($1,$2,'00000000-0000-7000-8000-000000000021','00000000-0000-7000-8000-000000000011',1,'mass',100)", []any{platform.ID(), recipe}},
	}
	for _, s := range statements {
		if _, err = pool.Exec(ctx, s.sql, s.args...); err != nil {
			return err
		}
	}
	stage = "real River worker startup"
	workers := river.NewWorkers()
	river.AddWorker(workers, &copyWorker{dispatcher: d})
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Queues: map[string]river.QueueConfig{"recovery": {MaxWorkers: 1}}, Workers: workers, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		return err
	}
	if err = client.Start(ctx); err != nil {
		return err
	}
	defer client.Stop(context.Background())
	payload, _ := json.Marshal(map[string]any{"sourceSpaceId": source, "targetSpaceId": target, "userId": user, "lists": true, "lots": true, "recipes": true})
	key := "recovery-copy:" + platform.ID()
	inserted, err := client.Insert(ctx, platform.Event{Event: "space.copy", Key: key, Payload: payload}, &river.InsertOpts{Queue: "recovery", MaxAttempts: 3})
	if err != nil {
		return err
	}
	wait := func(attempt int) (*rivertype.JobRow, error) {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			j, e := client.JobGet(ctx, inserted.Job.ID)
			if e != nil {
				return nil, e
			}
			if j.State == rivertype.JobStateCompleted && j.Attempt >= attempt {
				return j, nil
			}
			if j.State == rivertype.JobStateDiscarded {
				return nil, errors.New("job discarded")
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
			}
		}
	}
	stage = "first business worker execution"
	first, err := wait(1)
	if err != nil {
		return err
	}
	fingerprint := func() (json.RawMessage, error) {
		var b []byte
		e := pool.QueryRow(ctx, `WITH rows AS (
SELECT 'list' kind,to_jsonb(x) row FROM shopping.shopping_lists x WHERE space_id=$1 UNION ALL
SELECT 'item',to_jsonb(x) FROM shopping.shopping_items x WHERE space_id=$1 UNION ALL
SELECT 'lot',to_jsonb(x) FROM pantry.food_lots x WHERE space_id=$1 UNION ALL
SELECT 'movement',to_jsonb(x) FROM pantry.stock_movements x WHERE space_id=$1 UNION ALL
SELECT 'recipe',to_jsonb(x) FROM recipe.recipes x WHERE space_id=$1 UNION ALL
SELECT 'ingredient',to_jsonb(x) FROM recipe.recipe_ingredients x JOIN recipe.recipes r ON r.id=x.recipe_id WHERE r.space_id=$1)
SELECT jsonb_build_object('sha256',encode(sha256(convert_to(coalesce(string_agg(row::text,'' ORDER BY kind,row::text),''),'UTF8')),'hex'),
'lists',(SELECT count(*) FROM shopping.shopping_lists WHERE space_id=$1),'items',(SELECT count(*) FROM shopping.shopping_items WHERE space_id=$1),
'lots',(SELECT count(*) FROM pantry.food_lots WHERE space_id=$1),'movements',(SELECT count(*) FROM pantry.stock_movements WHERE space_id=$1),
'recipes',(SELECT count(*) FROM recipe.recipes WHERE space_id=$1),'remainingBase',(SELECT sum(base_remaining)::text FROM pantry.food_lots WHERE space_id=$1),
'totalCostVND',(SELECT sum(total_cost)::text FROM pantry.food_lots WHERE space_id=$1)) FROM rows`, target).Scan(&b)
		return b, e
	}
	before, err := fingerprint()
	if err != nil {
		return err
	}
	var values struct {
		Lists, Items, Lots, Movements, Recipes int
		RemainingBase, TotalCostVND            string
	}
	if err = json.Unmarshal(before, &values); err != nil {
		return err
	}
	if values.Lists != 1 || values.Items != 1 || values.Lots != 1 || values.Movements != 1 || values.Recipes != 1 || values.RemainingBase != "500.000000" || values.TotalCostVND != "10000" {
		return errors.New("missing copied business effect")
	}
	stage = "retry same completed business job"
	if _, err = client.JobRetry(ctx, inserted.Job.ID); err != nil {
		return err
	}
	second, err := wait(first.Attempt + 1)
	if err != nil {
		return err
	}
	after, err := fingerprint()
	if err != nil {
		return err
	}
	if string(before) != string(after) {
		return errors.New("duplicate business effect")
	}
	var receipts int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM platform.worker_receipts WHERE event_key=$1", key).Scan(&receipts); err != nil || receipts != 1 {
		return errors.New("receipt not idempotent")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "PASS", "event": "space.copy", "queue": "recovery", "sameJobRetried": true, "firstAttempt": first.Attempt, "retryAttempt": second.Attempt, "businessFingerprintUnchanged": true, "businessState": json.RawMessage(after), "workerReceipts": receipts, "usesProductionDispatcher": true, "usesExistingSigningOrQueueKeys": false})
}
'''


def main():
    started = time.monotonic()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--postgres-container', default='dicho-qa-postgres-1')
    parser.add_argument('--app-container', default='dicho-qa-app-1')
    parser.add_argument('--source-db', default='dicho_test')
    parser.add_argument('--user', default='dicho')
    parser.add_argument('--report', default=str(ROOT/'output/backend-qa/recovery.json'))
    parser.add_argument('--skip-worker-retry', action='store_true')
    args = parser.parse_args()
    recovery = Recovery(args)
    try:
        recovery.execute()
    except (RecoveryError, ValueError, OSError) as error:
        recovery.result['error'] = str(error) if isinstance(error, RecoveryError) else type(error).__name__
        recovery.result['failureStage'] = recovery.phase
    finally:
        try:
            recovery.cleanup()
        except (RecoveryError, OSError) as error:
            recovery.result.update(status='FAIL', cleanupError=type(error).__name__)
        recovery.result['durationSeconds'] = round(time.monotonic()-started, 6)
        report = Path(args.report).resolve()
        report.parent.mkdir(parents=True, exist_ok=True)
        fd, temporary = tempfile.mkstemp(prefix='.recovery-', dir=report.parent)
        with os.fdopen(fd, 'w') as output:
            json.dump(recovery.result, output, ensure_ascii=False, indent=2)
            output.write('\n')
        os.replace(temporary, report)
    print('Recovery '+recovery.result['status']+'; evidence: '+str(report))
    if recovery.result['status'] != 'PASS':
        print('Failure stage: '+str(recovery.result.get('failureStage', 'cleanup')))
    return 0 if recovery.result['status'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
