import json
from pathlib import Path
from subprocess import CompletedProcess

import pytest

from proofread_assist.cli import main
from proofread_assist.codex import analyze_codex


def test_codex_uses_chatgpt_cli_without_exposing_site_or_api_credentials(monkeypatch):
    monkeypatch.setenv('OPENAI_API_KEY', 'MODEL_SECRET')
    monkeypatch.setattr('proofread_assist.codex.shutil.which', lambda name: '/usr/bin/codex')
    text = 'В докладе Сараговского комитета.'

    def fake_run(command, **kwargs):
        assert command[0:2] == ['/usr/bin/codex', 'exec']
        assert '--ephemeral' in command
        assert '--ignore-user-config' in command
        assert command[command.index('-s') + 1] == 'read-only'
        assert command[-1] == '-'
        assert kwargs['env'].get('OPENAI_API_KEY') is None
        assert 'MODEL_SECRET' not in kwargs['input']
        assert text in kwargs['input']
        assert 'site-token' not in kwargs['input']
        output = Path(command[command.index('--output-last-message') + 1])
        output.write_text(json.dumps({'findings': [{
            'start_rune': 10, 'end_rune': 22, 'old': 'Сараговского',
            'new': 'Саратовского', 'reason': 'Сверить со сканом',
        }]}))
        return CompletedProcess(command, 0)

    monkeypatch.setattr('proofread_assist.codex.subprocess.run', fake_run)
    found = analyze_codex(text)
    assert len(found) == 1
    assert found[0]['old'] == 'Сараговского'
    assert found[0]['new'] == 'Саратовского'
    assert found[0]['source'] == 'model'


def test_codex_vision_passes_scan_as_temporary_image(monkeypatch):
    monkeypatch.setattr('proofread_assist.codex.shutil.which', lambda name: '/usr/bin/codex')

    def fake_run(command, **kwargs):
        image = Path(command[command.index('-i') + 1])
        assert image.read_bytes() == b'png-data'
        assert image.suffix == '.png'
        assert 'scan_bbox' in kwargs['input']
        assert kwargs['encoding'] == 'utf-8'
        Path(command[command.index('--output-last-message') + 1]).write_text(json.dumps({
            'findings': [{
                'start_rune': 0, 'end_rune': 12, 'old': 'Сараговского',
                'new': 'Саратовского', 'reason': 'Сверено со сканом',
                'scan_bbox': [0.305, 0.472, 0.246, 0.026],
            }],
        }, ensure_ascii=False), encoding='utf-8')
        return CompletedProcess(command, 0)

    monkeypatch.setattr('proofread_assist.codex.subprocess.run', fake_run)
    found = analyze_codex('Сараговского', (b'png-data', 'image/png'))
    assert found[0]['scan_bbox'] == [0.305, 0.472, 0.246, 0.026]


def test_codex_missing_or_invalid_output_does_not_upload_invented_findings(monkeypatch):
    monkeypatch.setattr('proofread_assist.codex.shutil.which', lambda name: None)
    with pytest.raises(ValueError, match='Codex CLI'):
        analyze_codex('Текст.')

    monkeypatch.setattr('proofread_assist.codex.shutil.which', lambda name: '/usr/bin/codex')
    monkeypatch.setattr('proofread_assist.codex.subprocess.run',
                        lambda command, **_kwargs: CompletedProcess(command, 0))
    with pytest.raises(ValueError):
        analyze_codex('Текст.')

    monkeypatch.setattr('proofread_assist.codex.subprocess.run',
                        lambda command, **_kwargs: CompletedProcess(command, 1, stderr='secret'))
    with pytest.raises(ValueError, match='Codex CLI завершился') as failure:
        analyze_codex('Текст.')
    assert 'secret' not in str(failure.value)


def test_run_with_codex_uses_existing_pairing_without_model_configuration(serve, monkeypatch, tmp_path):
    monkeypatch.setenv('PROOFREAD_ASSIST_HOME', str(tmp_path))
    monkeypatch.setattr('proofread_assist.cli.shutil.which', lambda _name: '/usr/bin/codex')
    calls, uploads = [], []

    def site(method, path, _headers, body):
        if path.endswith('/pair'):
            return 200, {'run_id': 3, 'token': 'site-token'}
        if method == 'GET' and path.endswith('after=0'):
            return 200, {'page_id': 10, 'page_number': 1, 'analysis_status': 'pending',
                         'base_markdown': 'Текст.', 'base_sha256': 'hash'}
        if method == 'GET':
            return 204, None
        uploads.append(body)
        return 204, None

    monkeypatch.setattr('proofread_assist.cli.analyze_codex',
                        lambda text, scan=None: calls.append((text, scan)) or [])
    assert main(['pair', 'once', '--site-url', serve(site)]) == 0
    assert main(['run', '3', '--codex']) == 0
    assert calls == [('Текст.', None)]
    assert uploads == [{'findings': [], 'base_sha256': 'hash'}]
    assert main(['run', '3', '--codex', '--model-url', 'http://example.test/v1',
                 '--model', 'demo']) == 1


def test_missing_codex_stops_before_fetching_pages(serve, monkeypatch, tmp_path):
    monkeypatch.setenv('PROOFREAD_ASSIST_HOME', str(tmp_path))
    requests = []

    def site(_method, path, _headers, _body):
        requests.append(path)
        if path.endswith('/pair'):
            return 200, {'run_id': 3, 'token': 'site-token'}
        return 204, None

    assert main(['pair', 'once', '--site-url', serve(site)]) == 0
    monkeypatch.setattr('proofread_assist.cli.shutil.which', lambda _name: None)
    assert main(['run', '3', '--codex']) == 1
    assert len(requests) == 1
