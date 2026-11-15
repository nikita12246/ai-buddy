"""Start and verify the local backend demo in a dedicated Compose project."""

import argparse
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import subprocess
import sys
from tempfile import TemporaryDirectory

if __package__:
    from .backend_demo_smoke import smoke
else:
    from backend_demo_smoke import smoke

ROOT = Path(__file__).resolve().parents[2]
CREDENTIALS = ROOT / '.env.backend-demo'
CREDENTIAL_KEYS = (
    'BACKEND_DEMO_DB_PASSWORD',
    'BACKEND_DEMO_ATLAS_TOKEN',
    'BACKEND_DEMO_ORBIT_TOKEN',
    'BACKEND_DEMO_ACCESS_TOKEN',
    'BACKEND_DEMO_DECISION_TOKEN',
)
PROJECT = 'ai-buddy-backend-demo'


def model_environment(path):
    fields = ('LLM_API_KEY', 'LLM_PROVIDER', 'LLM_MODEL')
    values = {}
    if path.is_file():
        for line in path.read_text(encoding='utf-8-sig').splitlines():
            name, equals, value = line.strip().removeprefix('export ').partition('=')
            name = name.strip()
            if not equals or name not in fields:
                continue
            try:
                parts = shlex.split(value, comments=True)
                if len(parts) > 1:
                    raise ValueError
            except ValueError as error:
                raise ValueError(f'Проверьте формат {name} в .env.') from error
            values[name] = parts[0] if parts else ''
    values.update({name: os.environ[name] for name in fields if name in os.environ})
    return values


def credentials(create):
    if create:
        try:
            fd = os.open(CREDENTIALS, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        except FileExistsError:
            pass
        else:
            with os.fdopen(fd, 'w', encoding='ascii') as file:
                for key in CREDENTIAL_KEYS:
                    file.write(f'{key}={secrets.token_hex(32)}\n')
    try:
        values = dict(line.split('=', 1) for line in CREDENTIALS.read_text(encoding='ascii').splitlines() if line)
    except FileNotFoundError as error:
        raise ValueError('Сначала запустите backend_demo.py up.') from error
    except (ValueError, UnicodeError) as error:
        raise ValueError('Файл .env.backend-demo не содержит корректных локальных настроек.') from error
    if set(values) != set(CREDENTIAL_KEYS) or not all(re.fullmatch('[0-9a-f]{64}', value) for value in values.values()):
        raise ValueError('Файл .env.backend-demo не содержит корректных локальных настроек.')
    return values


def compose(env, *args, **options):
    return subprocess.run(
        ['docker', 'compose', '--project-name', PROJECT, '-f', 'compose.backend.yaml', *args],
        cwd=ROOT, env=env, check=True, **options,
    )


def seed(full, raw_dir):
    if full:
        command = [sys.executable, '-m', 'scripts.ingest.build']
        if raw_dir is not None:
            command += ['--raw-dir', str(raw_dir.resolve())]
        subprocess.run(command, cwd=ROOT, check=True)
        corpus = ROOT / 'data/processed/corpus.jsonl'
        sql = (ROOT / 'data/processed/knowledge-seed.sql').read_bytes()
    else:
        sys.path.insert(0, str(ROOT))
        from scripts.ingest.checklist import write_jsonl
        from scripts.ingest.corpus import public_chunks
        from scripts.ingest.seed import render_seed

        manifest = ROOT / 'data/public-documents.json'
        policies = ROOT / 'data/synthetic/policies.jsonl'
        chunks = [json.loads(line) for line in policies.read_text(encoding='utf-8').splitlines() if line.strip()]
        for entry in json.loads(manifest.read_text(encoding='utf-8')):
            if entry['kind'] == 'summary':
                chunks.extend(public_chunks(ROOT / 'data/raw', entry, manifest.parent))
        with TemporaryDirectory() as temporary:
            corpus = Path(temporary) / 'demo-corpus.jsonl'
            write_jsonl(corpus, chunks)
            sql = render_seed(corpus).encode('utf-8')
        count = len(chunks)
    if not sql.startswith(b'BEGIN;\n'):
        raise ValueError('Подготовленный SQL должен начинать транзакцию.')
    # Replace only the search index in this dedicated demo database. Leave
    # conversations, drafts and operations intact across restarts.
    sql = sql.replace(b'BEGIN;\n', b'BEGIN;\nTRUNCATE TABLE knowledge_chunks;\n', 1)
    if full:
        count = sum(bool(line.strip()) for line in corpus.read_bytes().splitlines())
    return sql, count


def initialize(env, corpus_sql, expected):
    psql = ('exec', '-T', 'postgres', 'psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1',
            '-U', 'ai_buddy_demo', '-d', 'ai_buddy_demo')
    for path in sorted((ROOT / 'migrations').glob('[0-9][0-9][0-9]_*.sql')):
        compose(env, *psql, input=path.read_bytes())
    schema = ROOT / 'backend/internal/knowledge/schema.sql'
    compose(env, *psql, input=schema.read_bytes())
    compose(env, *psql, input=corpus_sql)
    loaded = compose(env, *psql, '-At', '-c', 'SELECT count(*) FROM knowledge_chunks', capture_output=True).stdout.strip()
    if loaded != str(expected).encode():
        raise ValueError('Число загруженных фрагментов не совпадает с подготовленным снимком.')
    print(f'Загружено {expected} фрагментов.', flush=True)


def port(env, key, default):
    raw = env.get(key, default)
    if not re.fullmatch('[0-9]{1,5}', raw) or not 1 <= int(raw) <= 65535:
        raise ValueError(f'{key} должен содержать номер локального порта.')
    return int(raw)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    up = commands.add_parser('up', help='start the database, load knowledge and start the API')
    up.add_argument('--full', action='store_true', help='build the full locked corpus from local inputs')
    up.add_argument('--raw-dir', type=Path, help='override the raw snapshot folder for --full')
    up.add_argument('--live', action='store_true', help='call the configured LLM provider')
    up.add_argument('--smoke', action='store_true', help='run the deterministic HTTP smoke after startup')
    smoke_command = commands.add_parser('smoke', help='check the running demo with its local tokens')
    smoke_command.add_argument('--verify-restart', action='store_true', help='also check data after recreating the demo containers')
    commands.add_parser('down', help='stop the demo and retain its local database')
    commands.add_parser('logs', help='show recent logs for the demo services')
    commands.add_parser('config', help='validate the Compose configuration without starting containers')
    args = parser.parse_args()
    live = getattr(args, 'live', False)
    if live and args.smoke:
        parser.error('--smoke использует учебный режим. Для живого режима запустите up --live.')
    if getattr(args, 'raw_dir', None) is not None and not args.full:
        parser.error('--raw-dir применяется вместе с --full.')

    try:
        model = model_environment(ROOT / '.env') if live else {'LLM_API_KEY': ''}
        if live and not model.get('LLM_API_KEY', '').strip():
            parser.error('Задайте LLM_API_KEY в .env или в окружении для --live.')
        values = credentials(create=args.command in {'up', 'config'})
        env = {**os.environ, **values, **model}
        api_port = port(env, 'BACKEND_DEMO_API_PORT', '8081')
        mock_port = port(env, 'BACKEND_DEMO_MOCK_PORT', '8082')
        web_port = port(env, 'BACKEND_DEMO_WEB_PORT', '8080')
        if len({api_port, mock_port, web_port}) != 3:
            raise ValueError('Укажите разные локальные порты для интерфейса, API и сервиса заявок.')
        env.update(BACKEND_DEMO_API_PORT=str(api_port), BACKEND_DEMO_MOCK_PORT=str(mock_port),
                   BACKEND_DEMO_WEB_PORT=str(web_port))
        web_url, mock_url = f'http://127.0.0.1:{web_port}', f'http://127.0.0.1:{mock_port}'
        if args.command == 'config':
            compose(env, 'config', '--quiet')
        elif args.command == 'down':
            compose(env, 'down')
        elif args.command == 'logs':
            compose(env, 'logs', '--no-color', '--tail', '80')
        elif args.command == 'up':
            corpus_sql, expected = seed(args.full, args.raw_dir)
            compose(env, 'up', '-d', '--wait', '--wait-timeout', '90', 'postgres')
            initialize(env, corpus_sql, expected)
            compose(env, 'build', 'backend', 'web')
            compose(env, 'up', '-d', '--wait', '--wait-timeout', '90', '--no-build', 'web')
            print(f'AI-buddy: {web_url}. Сайт откроется без ввода токена.', flush=True)
        if args.command == 'smoke' or getattr(args, 'smoke', False):
            def restart():
                compose(env, 'down')
                compose(env, 'up', '-d', '--wait', '--wait-timeout', '90', '--no-build', 'web')

            smoke(web_url, mock_url, values['BACKEND_DEMO_ATLAS_TOKEN'], values['BACKEND_DEMO_ORBIT_TOKEN'],
                  values['BACKEND_DEMO_DECISION_TOKEN'],
                  restart=restart if getattr(args, 'verify_restart', False) else None)
    except subprocess.CalledProcessError as error:
        parser.exit(1, f'Команда запуска завершилась с кодом {error.returncode}. Проверьте вывод выше.\n')
    except (OSError, ValueError, RuntimeError) as error:
        parser.exit(1, f'{error}\n')


if __name__ == '__main__':
    main()
