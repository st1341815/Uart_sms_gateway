from flask import Flask, Response, request
import os, json, requests, yaml

BACKEND = os.environ.get('BACKEND', 'http://newsms:8080')
CONFIG = os.environ.get('CONFIG_PATH', '/config/config.yaml')

app = Flask(__name__)

# load device mapping from config.yaml
DEVICE_MAP = {}
try:
    with open(CONFIG, 'r', encoding='utf-8') as f:
        cfg = yaml.safe_load(f) or {}
    devices = (((cfg.get('App') or {}).get('Serial') or {}).get('Devices') or [])
    for d in devices:
        did = str(d.get('ID') or '').strip()
        name = str(d.get('Name') or did).strip()
        if did:
            DEVICE_MAP[did] = name
except Exception as e:
    DEVICE_MAP = {}

def badge(device_id: str) -> str:
    if not device_id:
        return ''
    name = DEVICE_MAP.get(device_id, device_id)
    return f"[{name}]"

def upstream(path: str):
    url = BACKEND + path
    headers = {}
    # forward auth + relevant headers
    for k in ['Authorization', 'Content-Type']:
        if k in request.headers:
            headers[k] = request.headers[k]
    resp = requests.request(
        method=request.method,
        url=url,
        params=request.args,
        data=request.get_data() if request.method not in ('GET','HEAD') else None,
        headers=headers,
        timeout=15,
    )
    return resp

@app.route('/api/<path:subpath>', methods=['GET','POST','PUT','DELETE','PATCH','OPTIONS'])
def proxy_api(subpath):
    path = '/api/' + subpath
    resp = upstream(path)
    ct = resp.headers.get('Content-Type','')

    # Only rewrite JSON for messages endpoints
    if resp.status_code == 200 and 'application/json' in ct and (
        subpath.startswith('messages/conversations')
    ):
        try:
            data = resp.json()

            # conversations list
            if subpath == 'messages/conversations':
                for conv in data:
                    lm = conv.get('lastMessage') or {}
                    did = lm.get('deviceId') or ''
                    b = badge(did)
                    if b and isinstance(lm.get('content'), str) and not lm['content'].startswith(b):
                        lm['content'] = b + lm['content']
                        conv['lastMessage'] = lm

            # messages list under a peer
            elif subpath.startswith('messages/conversations/') and subpath.endswith('/messages') and isinstance(data, list):
                for m in data:
                    did = m.get('deviceId') or ''
                    b = badge(did)
                    if b and isinstance(m.get('content'), str) and not m['content'].startswith(b):
                        m['content'] = b + m['content']

            out = json.dumps(data, ensure_ascii=False)
            return Response(out, status=resp.status_code, mimetype='application/json')
        except Exception:
            pass

    # passthrough
    excluded = {'content-encoding', 'transfer-encoding', 'connection'}
    headers = [(k,v) for k,v in resp.headers.items() if k.lower() not in excluded]
    return Response(resp.content, status=resp.status_code, headers=headers)

# health
@app.get('/healthz')
def healthz():
    return {'ok': True, 'deviceMap': DEVICE_MAP}



if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000)
