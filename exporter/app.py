from flask import Flask, Response, request
import sqlite3, csv, io, os, json, datetime

DB_PATH = os.environ.get('DB_PATH','/data/app.db')

app = Flask(__name__)

def q(sql, params=()):
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    cur = conn.cursor()
    rows = cur.execute(sql, params).fetchall()
    conn.close()
    return rows

@app.get('/')
def index():
    return (
        "<html><body>"
        "<h3>NewSMS 导出</h3>"
        "<ul>"
        "<li><a href='/export/messages.csv'>导出短信(messages.csv)</a></li>"
        "<li><a href='/export/messages.json'>导出短信(messages.json)</a></li>"
        "</ul>"
        "<p>字段包含 device_id（用于区分哪张 SIM 卡收发）。</p>"
        "</body></html>"
    )

@app.get('/export/messages.json')
def export_json():
    rows = q('select id, "from", "to", content, type, status, created_at, updated_at, device_id from text_messages order by created_at asc')
    data = [dict(r) for r in rows]
    return Response(json.dumps(data, ensure_ascii=False), mimetype='application/json')

@app.get('/export/messages.csv')
def export_csv():
    rows = q('select id, "from", "to", content, type, status, created_at, updated_at, device_id from text_messages order by created_at asc')
    out = io.StringIO()
    w = csv.writer(out)
    w.writerow(['id','from','to','content','type','status','created_at','updated_at','device_id'])
    for r in rows:
        w.writerow([r['id'], r['from'], r['to'], r['content'], r['type'], r['status'], r['created_at'], r['updated_at'], r['device_id']])
    csv_bytes = out.getvalue().encode('utf-8-sig')
    return Response(
        csv_bytes,
        mimetype='text/csv; charset=utf-8',
        headers={'Content-Disposition':'attachment; filename="messages.csv"'}
    )

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000)
