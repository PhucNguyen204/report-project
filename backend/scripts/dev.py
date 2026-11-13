#!/usr/bin/env python3
import argparse
import os
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def read_env(path):
    values = {}
    for number, raw in enumerate(path.read_text().splitlines(), 1):
        if not raw.strip() or raw.lstrip().startswith('#'):
            continue
        key, separator, value = raw.partition('=')
        key, value = key.strip(), value.strip()
        if not separator or not re.fullmatch(r'[A-Z_][A-Z0-9_]*', key):
            raise ValueError(f'invalid environment assignment at line {number}')
        if len(value) >= 2 and value[0] in "\"'" and value[-1] == value[0]:
            value = value[1:-1]
        values[key] = value
    return values


def initialize():
    subprocess.run(['go', 'run', './cmd/keygen', '.local'], cwd=ROOT, check=True)
    try:
        fd = os.open(ROOT / '.env', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError:
        return
    with os.fdopen(fd, 'w') as output:
        output.write((ROOT / '.env.example').read_text())
        output.write(f'LOCAL_UID={os.getuid()}\nLOCAL_GID={os.getgid()}\n')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('command', choices=['init', 'run'])
    args = parser.parse_args()
    if args.command == 'init':
        initialize()
    else:
        env = {**read_env(ROOT / '.env'), **os.environ}
        os.chdir(ROOT)
        os.execve(ROOT / 'bin/dicho', ['dicho'], env)


if __name__ == '__main__':
    main()
