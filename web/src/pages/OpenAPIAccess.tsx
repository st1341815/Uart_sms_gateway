import {useState} from 'react';
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query';
import {toast} from 'sonner';
import {
    AlertTriangle,
    Code2,
    Copy,
    FileText,
    KeyRound,
    Plus,
    RefreshCw,
    Trash2,
} from 'lucide-react';
import {Button} from '@/components/ui/button';
import {Input} from '@/components/ui/input';
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from '@/components/ui/card';
import {
    createKey,
    createTemplate,
    deleteKey,
    deleteTemplate,
    listKeys,
    listSendLogs,
    listTemplates,
    resetSecret,
    updateKey,
    updateTemplate,
    type APIKey,
    type SMSTemplate,
} from '../api/openapi';
import {getDevicesList} from '../api/serial';

function copy(text: string) {
    navigator.clipboard?.writeText(text).then(
        () => toast.success('已复制'),
        () => toast.error('复制失败，请手动选择'),
    );
}

export default function OpenAPIAccess() {
    const qc = useQueryClient();
    const [tab, setTab] = useState<'keys' | 'templates' | 'docs' | 'logs'>('keys');

    // 新建 Key 后展示一次性 Secret
    const [newSecret, setNewSecret] = useState<{ accessKey: string; secret: string } | null>(null);

    const {data: keysResp, isLoading: keysLoading} = useQuery({
        queryKey: ['openapi-keys'],
        queryFn: listKeys,
    });
    const {data: tplResp} = useQuery({
        queryKey: ['openapi-templates'],
        queryFn: listTemplates,
    });
    const {data: devicesResp} = useQuery({
        queryKey: ['devices-for-openapi'],
        queryFn: getDevicesList,
        staleTime: 60000,
    });
    const {data: logsResp} = useQuery({
        queryKey: ['openapi-logs'],
        queryFn: () => listSendLogs(50),
        enabled: tab === 'logs',
        refetchInterval: tab === 'logs' ? 5000 : false,
    });

    const keys = keysResp?.keys ?? [];
    const templates = tplResp?.templates ?? [];
    const devices = devicesResp?.devices ?? [];

    // ---------- Key 操作 ----------
    const [keyForm, setKeyForm] = useState({name: '', deviceId: '', allowedIps: '', rateLimitPerMinute: 20});
    const [showKeyForm, setShowKeyForm] = useState(false);

    const createKeyMut = useMutation({
        mutationFn: createKey,
        onSuccess: (res) => {
            setNewSecret({accessKey: res.key.accessKey, secret: res.secret});
            setShowKeyForm(false);
            setKeyForm({name: '', deviceId: '', allowedIps: '', rateLimitPerMinute: 20});
            qc.invalidateQueries({queryKey: ['openapi-keys']});
            toast.success('已创建。请立即保存 Secret');
        },
        onError: (e: any) => toast.error(e?.message || '创建失败'),
    });

    const toggleKeyMut = useMutation({
        mutationFn: (k: APIKey) => updateKey(k.id, {
            name: k.name, deviceId: k.deviceId, allowedIps: k.allowedIps,
            rateLimitPerMinute: k.rateLimitPerMinute, enabled: !k.enabled,
        }),
        onSuccess: () => {
            qc.invalidateQueries({queryKey: ['openapi-keys']});
            toast.success('已更新');
        },
        onError: (e: any) => toast.error(e?.message || '更新失败'),
    });

    const deleteKeyMut = useMutation({
        mutationFn: deleteKey,
        onSuccess: () => {
            qc.invalidateQueries({queryKey: ['openapi-keys']});
            toast.success('已删除');
        },
        onError: (e: any) => toast.error(e?.message || '删除失败'),
    });

    const resetMut = useMutation({
        mutationFn: resetSecret,
        onSuccess: (res, id) => {
            const k = keys.find((x) => x.id === id);
            setNewSecret({accessKey: k?.accessKey || '', secret: res.secret});
            qc.invalidateQueries({queryKey: ['openapi-keys']});
            toast.success('Secret 已重置');
        },
        onError: (e: any) => toast.error(e?.message || '重置失败'),
    });

    // ---------- 模板操作 ----------
    const [tplForm, setTplForm] = useState({id: '', code: '', name: '', content: '', description: '', enabled: true});
    const [showTplForm, setShowTplForm] = useState(false);

    const saveTplMut = useMutation({
        mutationFn: async () => {
            const req = {
                code: tplForm.code, name: tplForm.name, content: tplForm.content,
                description: tplForm.description, enabled: tplForm.enabled,
            };
            return tplForm.id ? updateTemplate(tplForm.id, req) : createTemplate(req);
        },
        onSuccess: () => {
            qc.invalidateQueries({queryKey: ['openapi-templates']});
            setShowTplForm(false);
            setTplForm({id: '', code: '', name: '', content: '', description: '', enabled: true});
            toast.success('已保存');
        },
        onError: (e: any) => toast.error(e?.message || '保存失败'),
    });

    const deleteTplMut = useMutation({
        mutationFn: deleteTemplate,
        onSuccess: () => {
            qc.invalidateQueries({queryKey: ['openapi-templates']});
            toast.success('已删除');
        },
        onError: (e: any) => toast.error(e?.message || '删除失败'),
    });

    const tabs = [
        {key: 'keys', label: 'API 密钥', icon: KeyRound},
        {key: 'templates', label: '短信模板', icon: FileText},
        {key: 'docs', label: '接入文档', icon: Code2},
        {key: 'logs', label: '调用记录', icon: RefreshCw},
    ] as const;

    return (
        <div className="max-w-6xl mx-auto">
            <div className="mb-6">
                <h1 className="text-2xl font-bold text-gray-900">开放接口</h1>
                <p className="text-sm text-gray-500 mt-1">
                    其他应用可通过 AK/SK 签名调用本接口发送短信验证码
                </p>
            </div>

            {/* 一次性 Secret 提示 */}
            {newSecret && (
                <Card className="mb-6 border-amber-300 bg-amber-50">
                    <CardHeader className="pb-3">
                        <CardTitle className="text-base flex items-center gap-2 text-amber-900">
                            <AlertTriangle className="w-5 h-5"/>
                            Secret 只显示这一次，请立即保存
                        </CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-2">
                        <div className="flex items-center gap-2">
                            <span className="text-xs text-gray-600 w-24">AccessKey</span>
                            <code className="flex-1 bg-white px-3 py-1.5 rounded border text-xs font-mono break-all">
                                {newSecret.accessKey}
                            </code>
                            <Button size="sm" variant="outline" onClick={() => copy(newSecret.accessKey)}>
                                <Copy className="w-3.5 h-3.5"/>
                            </Button>
                        </div>
                        <div className="flex items-center gap-2">
                            <span className="text-xs text-gray-600 w-24">SecretKey</span>
                            <code className="flex-1 bg-white px-3 py-1.5 rounded border text-xs font-mono break-all">
                                {newSecret.secret}
                            </code>
                            <Button size="sm" variant="outline" onClick={() => copy(newSecret.secret)}>
                                <Copy className="w-3.5 h-3.5"/>
                            </Button>
                        </div>
                        <Button size="sm" variant="ghost" onClick={() => setNewSecret(null)}>我已保存，关闭</Button>
                    </CardContent>
                </Card>
            )}

            {/* Tab */}
            <div className="flex gap-1 mb-4 border-b">
                {tabs.map((t) => {
                    const Icon = t.icon;
                    const active = tab === t.key;
                    return (
                        <button
                            key={t.key}
                            onClick={() => setTab(t.key)}
                            className={`flex items-center gap-1.5 px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
                                active ? 'border-blue-600 text-blue-600' : 'border-transparent text-gray-500 hover:text-gray-700'
                            }`}
                        >
                            <Icon className="w-4 h-4"/>
                            {t.label}
                        </button>
                    );
                })}
            </div>

            {/* ===== 密钥 ===== */}
            {tab === 'keys' && (
                <div className="space-y-4">
                    <div className="flex justify-end">
                        <Button onClick={() => setShowKeyForm(!showKeyForm)} size="sm">
                            <Plus className="w-4 h-4 mr-1.5"/>新建密钥
                        </Button>
                    </div>

                    {showKeyForm && (
                        <Card>
                            <CardContent className="pt-5 space-y-3">
                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                    <div>
                                        <label className="text-xs text-gray-600 mb-1 block">名称 *</label>
                                        <Input value={keyForm.name}
                                               onChange={(e) => setKeyForm({...keyForm, name: e.target.value})}
                                               placeholder="如：HVAccRM 验证码"/>
                                    </div>
                                    <div>
                                        <label className="text-xs text-gray-600 mb-1 block">发送设备 *</label>
                                        <select
                                            value={keyForm.deviceId}
                                            onChange={(e) => setKeyForm({...keyForm, deviceId: e.target.value})}
                                            className="w-full h-10 border rounded-lg px-3 text-sm bg-gray-50"
                                        >
                                            <option value="">请选择设备</option>
                                            {devices.map((d) => (
                                                <option key={d.id} value={d.id}>
                                                    {d.name || d.id}（{d.id}）
                                                </option>
                                            ))}
                                        </select>
                                    </div>
                                    <div>
                                        <label className="text-xs text-gray-600 mb-1 block">IP 白名单（逗号分隔，留空不限制）</label>
                                        <Input value={keyForm.allowedIps}
                                               onChange={(e) => setKeyForm({...keyForm, allowedIps: e.target.value})}
                                               placeholder="192.168.11.0/24, 1.2.3.4"/>
                                    </div>
                                    <div>
                                        <label className="text-xs text-gray-600 mb-1 block">每分钟调用上限</label>
                                        <Input type="number" value={keyForm.rateLimitPerMinute}
                                               onChange={(e) => setKeyForm({...keyForm, rateLimitPerMinute: parseInt(e.target.value) || 20})}/>
                                    </div>
                                </div>
                                <div className="flex gap-2">
                                    <Button size="sm" disabled={createKeyMut.isPending}
                                            onClick={() => createKeyMut.mutate(keyForm)}>
                                        {createKeyMut.isPending ? '创建中...' : '创建'}
                                    </Button>
                                    <Button size="sm" variant="outline" onClick={() => setShowKeyForm(false)}>取消</Button>
                                </div>
                            </CardContent>
                        </Card>
                    )}

                    <Card>
                        <CardContent className="p-0">
                            {keysLoading ? (
                                <div className="p-8 text-center text-sm text-gray-500">加载中...</div>
                            ) : keys.length === 0 ? (
                                <div className="p-8 text-center text-sm text-gray-500">
                                    还没有密钥。点击「新建密钥」开始接入。
                                </div>
                            ) : (
                                <div className="divide-y">
                                    {keys.map((k) => (
                                        <div key={k.id} className="p-4 flex flex-wrap items-center gap-3">
                                            <div className="flex-1 min-w-[200px]">
                                                <div className="flex items-center gap-2">
                                                    <span className="font-medium text-sm text-gray-900">{k.name}</span>
                                                    <span className={`text-[10px] px-1.5 py-0.5 rounded border ${
                                                        k.enabled
                                                            ? 'bg-green-50 text-green-700 border-green-200'
                                                            : 'bg-gray-100 text-gray-500 border-gray-200'
                                                    }`}>
                                                        {k.enabled ? '启用' : '停用'}
                                                    </span>
                                                </div>
                                                <div className="text-xs text-gray-500 mt-1 font-mono break-all">
                                                    {k.accessKey} · {k.secretHint}
                                                </div>
                                                <div className="text-xs text-gray-400 mt-1">
                                                    设备 {k.deviceId} · {k.rateLimitPerMinute} 次/分
                                                    {k.allowedIps ? ` · IP: ${k.allowedIps}` : ' · 不限 IP'}
                                                    {k.lastUsedAt > 0 && ` · 最近 ${new Date(k.lastUsedAt).toLocaleString('zh-CN')}`}
                                                </div>
                                            </div>
                                            <div className="flex gap-1.5">
                                                <Button size="sm" variant="outline"
                                                        onClick={() => toggleKeyMut.mutate(k)}>
                                                    {k.enabled ? '停用' : '启用'}
                                                </Button>
                                                <Button size="sm" variant="outline"
                                                        onClick={() => {
                                                            if (confirm(`重置「${k.name}」的 Secret？旧 Secret 将立即失效。`))
                                                                resetMut.mutate(k.id);
                                                        }}>
                                                    重置 Secret
                                                </Button>
                                                <Button size="sm" variant="ghost" className="text-red-600"
                                                        onClick={() => {
                                                            if (confirm(`删除「${k.name}」？该操作不可恢复。`))
                                                                deleteKeyMut.mutate(k.id);
                                                        }}>
                                                    <Trash2 className="w-4 h-4"/>
                                                </Button>
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </CardContent>
                    </Card>
                </div>
            )}

            {/* ===== 模板 ===== */}
            {tab === 'templates' && (
                <div className="space-y-4">
                    <div className="flex justify-end">
                        <Button size="sm" onClick={() => {
                            setTplForm({id: '', code: '', name: '', content: '', description: '', enabled: true});
                            setShowTplForm(true);
                        }}>
                            <Plus className="w-4 h-4 mr-1.5"/>新建模板
                        </Button>
                    </div>

                    {showTplForm && (
                        <Card>
                            <CardContent className="pt-5 space-y-3">
                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                    <div>
                                        <label className="text-xs text-gray-600 mb-1 block">模板编码 *（调用时用）</label>
                                        <Input value={tplForm.code} placeholder="verify_code"
                                               onChange={(e) => setTplForm({...tplForm, code: e.target.value})}
                                               disabled={!!tplForm.id}/>
                                    </div>
                                    <div>
                                        <label className="text-xs text-gray-600 mb-1 block">模板名称 *</label>
                                        <Input value={tplForm.name} placeholder="登录验证码"
                                               onChange={(e) => setTplForm({...tplForm, name: e.target.value})}/>
                                    </div>
                                </div>
                                <div>
                                    <label className="text-xs text-gray-600 mb-1 block">
                                        模板内容 *（变量用 <code className="bg-gray-100 px-1">{'{'}code{'}'}</code> 形式）
                                    </label>
                                    <textarea
                                        value={tplForm.content}
                                        onChange={(e) => setTplForm({...tplForm, content: e.target.value})}
                                        rows={3}
                                        placeholder="您的验证码是{code}，5分钟内有效，请勿泄露。"
                                        className="w-full border rounded-lg px-3 py-2 text-sm bg-gray-50 outline-none focus:bg-white focus:border-blue-500 resize-none"
                                    />
                                </div>
                                <div>
                                    <label className="text-xs text-gray-600 mb-1 block">说明</label>
                                    <Input value={tplForm.description}
                                           onChange={(e) => setTplForm({...tplForm, description: e.target.value})}/>
                                </div>
                                <label className="flex items-center gap-2 text-sm">
                                    <input type="checkbox" checked={tplForm.enabled}
                                           onChange={(e) => setTplForm({...tplForm, enabled: e.target.checked})}/>
                                    启用
                                </label>
                                <div className="flex gap-2">
                                    <Button size="sm" disabled={saveTplMut.isPending}
                                            onClick={() => saveTplMut.mutate()}>
                                        {saveTplMut.isPending ? '保存中...' : '保存'}
                                    </Button>
                                    <Button size="sm" variant="outline" onClick={() => setShowTplForm(false)}>取消</Button>
                                </div>
                            </CardContent>
                        </Card>
                    )}

                    <Card>
                        <CardContent className="p-0">
                            {templates.length === 0 ? (
                                <div className="p-8 text-center text-sm text-gray-500">还没有模板</div>
                            ) : (
                                <div className="divide-y">
                                    {templates.map((t) => (
                                        <div key={t.id} className="p-4">
                                            <div className="flex items-start justify-between gap-3">
                                                <div className="flex-1">
                                                    <div className="flex items-center gap-2 flex-wrap">
                                                        <span className="font-medium text-sm text-gray-900">{t.name}</span>
                                                        <code className="text-xs bg-blue-50 text-blue-700 px-1.5 py-0.5 rounded">{t.code}</code>
                                                        {!t.enabled && (
                                                            <span className="text-[10px] px-1.5 py-0.5 rounded bg-gray-100 text-gray-500">已停用</span>
                                                        )}
                                                    </div>
                                                    <p className="text-xs text-gray-600 mt-1.5 break-words">{t.content}</p>
                                                    {t.params.length > 0 && (
                                                        <p className="text-xs text-gray-400 mt-1">
                                                            变量：{t.params.map((p) => `{${p}}`).join(' ')}
                                                        </p>
                                                    )}
                                                </div>
                                                <div className="flex gap-1.5 shrink-0">
                                                    <Button size="sm" variant="outline" onClick={() => {
                                                        setTplForm({
                                                            id: t.id, code: t.code, name: t.name, content: t.content,
                                                            description: t.description, enabled: t.enabled,
                                                        });
                                                        setShowTplForm(true);
                                                    }}>编辑</Button>
                                                    <Button size="sm" variant="ghost" className="text-red-600"
                                                            onClick={() => {
                                                                if (confirm(`删除模板「${t.name}」？`))
                                                                    deleteTplMut.mutate(t.id);
                                                            }}>
                                                        <Trash2 className="w-4 h-4"/>
                                                    </Button>
                                                </div>
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </CardContent>
                    </Card>
                </div>
            )}

            {/* ===== 文档 ===== */}
            {tab === 'docs' && <DocsPanel keys={keys}/>}

            {/* ===== 日志 ===== */}
            {tab === 'logs' && (
                <Card>
                    <CardContent className="p-0">
                        {(logsResp?.logs ?? []).length === 0 ? (
                            <div className="p-8 text-center text-sm text-gray-500">暂无调用记录</div>
                        ) : (
                            <div className="overflow-x-auto">
                                <table className="w-full text-xs">
                                    <thead className="bg-gray-50 text-gray-600">
                                    <tr>
                                        <th className="px-3 py-2 text-left font-medium whitespace-nowrap">时间</th>
                                        <th className="px-3 py-2 text-left font-medium whitespace-nowrap">密钥</th>
                                        <th className="px-3 py-2 text-left font-medium whitespace-nowrap">手机号</th>
                                        <th className="px-3 py-2 text-left font-medium whitespace-nowrap">模板</th>
                                        <th className="px-3 py-2 text-left font-medium whitespace-nowrap">状态</th>
                                        <th className="px-3 py-2 text-left font-medium whitespace-nowrap">来源 IP</th>
                                        <th className="px-3 py-2 text-left font-medium">原因</th>
                                    </tr>
                                    </thead>
                                    <tbody className="divide-y">
                                    {(logsResp?.logs ?? []).map((l) => (
                                        <tr key={l.id} className={l.status === 'rejected' ? 'bg-red-50/40' : ''}>
                                            <td className="px-3 py-2 whitespace-nowrap text-gray-500">
                                                {new Date(l.createdAt).toLocaleString('zh-CN')}
                                            </td>
                                            <td className="px-3 py-2 font-mono text-gray-600">{l.accessKey?.slice(0, 12)}…</td>
                                            <td className="px-3 py-2 font-mono">{l.phone || '—'}</td>
                                            <td className="px-3 py-2">{l.templateId || '—'}</td>
                                            <td className="px-3 py-2">
                                                <span className={`px-1.5 py-0.5 rounded text-[10px] ${
                                                    l.status === 'accepted'
                                                        ? 'bg-green-50 text-green-700'
                                                        : l.status === 'rejected'
                                                            ? 'bg-red-50 text-red-700'
                                                            : 'bg-gray-100 text-gray-600'
                                                }`}>{l.status}</span>
                                            </td>
                                            <td className="px-3 py-2 font-mono text-gray-500">{l.clientIp}</td>
                                            <td className="px-3 py-2 text-gray-600">{l.reason || '—'}</td>
                                        </tr>
                                    ))}
                                    </tbody>
                                </table>
                            </div>
                        )}
                    </CardContent>
                </Card>
            )}
        </div>
    );
}

// ---------- 接入文档 ----------
function DocsPanel({keys}: { keys: APIKey[] }) {
    const ak = keys[0]?.accessKey || '<你的 AccessKey>';

    const curlDemo = `#!/bin/bash
AK="${ak}"
SK="<你的 SecretKey>"
URL="http://192.168.11.21:8080/openapi/v1/sms/send"
BODY='{"phone":"13800138000","template":"verify_code","params":{"code":"123456"}}'

TS=$(date +%s)
NONCE=$(head -c 16 /dev/urandom | xxd -p)
BODY_HASH=$(printf '%s' "$BODY" | sha256sum | cut -d' ' -f1)

# StringToSign = METHOD\\nPATH\\nQUERY\\nBODY_SHA256\\nTIMESTAMP\\nNONCE
STS=$(printf 'POST\\n/openapi/v1/sms/send\\n\\n%s\\n%s\\n%s' "$BODY_HASH" "$TS" "$NONCE")
SIG=$(printf '%s' "$STS" | openssl dgst -sha256 -hmac "$SK" -hex | sed 's/^.* //')

curl -sS -X POST "$URL" \\
  -H "Content-Type: application/json" \\
  -H "X-Api-Key: $AK" \\
  -H "X-Timestamp: $TS" \\
  -H "X-Nonce: $NONCE" \\
  -H "X-Signature: $SIG" \\
  -d "$BODY"`;

    const pyDemo = `import hashlib, hmac, json, secrets, time, requests

AK = "${ak}"
SK = "<你的 SecretKey>"
BASE = "http://192.168.11.21:8080"

def send_sms(phone: str, template: str, params: dict) -> dict:
    path = "/openapi/v1/sms/send"
    body = json.dumps({"phone": phone, "template": template, "params": params},
                      ensure_ascii=False, separators=(",", ":"))
    ts = str(int(time.time()))
    nonce = secrets.token_hex(16)
    body_hash = hashlib.sha256(body.encode()).hexdigest()

    sts = "\\n".join(["POST", path, "", body_hash, ts, nonce])
    sig = hmac.new(SK.encode(), sts.encode(), hashlib.sha256).hexdigest()

    r = requests.post(BASE + path, data=body.encode(), timeout=10, headers={
        "Content-Type": "application/json",
        "X-Api-Key": AK,
        "X-Timestamp": ts,
        "X-Nonce": nonce,
        "X-Signature": sig,
    })
    return r.json()

if __name__ == "__main__":
    print(send_sms("13800138000", "verify_code", {"code": "123456"}))`;

    const jsDemo = `const crypto = require('crypto');

const AK = '${ak}';
const SK = '<你的 SecretKey>';
const BASE = 'http://192.168.11.21:8080';

async function sendSms(phone, template, params) {
  const path = '/openapi/v1/sms/send';
  const body = JSON.stringify({ phone, template, params });
  const ts = Math.floor(Date.now() / 1000).toString();
  const nonce = crypto.randomBytes(16).toString('hex');
  const bodyHash = crypto.createHash('sha256').update(body).digest('hex');

  const sts = ['POST', path, '', bodyHash, ts, nonce].join('\\n');
  const sig = crypto.createHmac('sha256', SK).update(sts).digest('hex');

  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Api-Key': AK,
      'X-Timestamp': ts,
      'X-Nonce': nonce,
      'X-Signature': sig,
    },
    body,
  });
  return res.json();
}

sendSms('13800138000', 'verify_code', { code: '123456' }).then(console.log);`;

    return (
        <div className="space-y-4">
            <Card>
                <CardHeader>
                    <CardTitle className="text-base">调用方式</CardTitle>
                    <CardDescription>接口地址：<code className="bg-gray-100 px-1.5 py-0.5 rounded">POST /openapi/v1/sms/send</code></CardDescription>
                </CardHeader>
                <CardContent className="text-sm text-gray-700 space-y-3">
                    <p><strong>请求头</strong></p>
                    <ul className="text-xs space-y-1 ml-4 list-disc text-gray-600">
                        <li><code>X-Api-Key</code> — 上面创建的 AccessKey</li>
                        <li><code>X-Timestamp</code> — Unix 秒级时间戳，与服务器偏差不能超过 <strong>5 分钟</strong></li>
                        <li><code>X-Nonce</code> — 随机串，每次请求必须唯一（防重放）</li>
                        <li><code>X-Signature</code> — HMAC-SHA256 十六进制小写</li>
                    </ul>

                    <p className="pt-2"><strong>签名算法</strong></p>
                    <pre className="bg-gray-900 text-gray-100 text-xs p-3 rounded-lg overflow-x-auto">{`StringToSign =
  METHOD + "\\n" +
  PATH + "\\n" +
  SORTED_QUERY + "\\n" +
  SHA256_HEX(body) + "\\n" +
  TIMESTAMP + "\\n" +
  NONCE

Signature = HMAC-SHA256(SecretKey, StringToSign)  →  hex 小写`}</pre>
                    <p className="text-xs text-gray-500">
                        SORTED_QUERY：query 参数按 key 升序拼成 <code>a=1&amp;b=2</code>，无参数则为空字符串。
                        PATH 不含域名与 query。
                    </p>

                    <p className="pt-2"><strong>请求体</strong></p>
                    <pre className="bg-gray-50 border text-xs p-3 rounded-lg overflow-x-auto">{`{
  "phone": "13800138000",
  "template": "verify_code",
  "params": { "code": "123456" }
}
// 或者不走模板：
// { "phone": "...", "content": "直接写完整内容" }`}</pre>

                    <p className="pt-2"><strong>响应</strong></p>
                    <pre className="bg-gray-50 border text-xs p-3 rounded-lg overflow-x-auto">{`// 成功
{ "success": true, "code": "OK", "message": "短信已提交发送",
  "data": { "messageId": "...", "phone": "138****8000", "deviceId": "sms1", "status": "accepted" } }

// 失败
{ "success": false, "code": "PHONE_TOO_FREQUENT", "message": "该手机号发送过于频繁，请 59 秒后重试" }`}</pre>

                    <p className="pt-2"><strong>错误码</strong></p>
                    <div className="text-xs text-gray-600 space-y-1">
                        {[
                            ['UNAUTHORIZED (401)', '签名/时间戳/非ce校验失败'],
                            ['INVALID_PHONE (400)', '手机号为空或格式错误'],
                            ['TEMPLATE_NOT_FOUND (400)', '模板不存在或已停用'],
                            ['TEMPLATE_PARAM_MISSING (400)', '缺少模板变量'],
                            ['PHONE_TOO_FREQUENT (429)', '同号码 60 秒内重复发送'],
                            ['PHONE_DAILY_LIMIT (429)', '同号码 24 小时超过 10 条'],
                            ['KEY_RATE_LIMIT (429)', '该 Key 超过每分钟上限'],
                            ['DEVICE_UNAVAILABLE (500)', '绑定设备不可用'],
                        ].map(([c, d]) => (
                            <div key={c} className="flex gap-2">
                                <code className="w-56 shrink-0 text-red-600">{c}</code>
                                <span>{d}</span>
                            </div>
                        ))}
                    </div>
                </CardContent>
            </Card>

            <Card>
                <CardHeader><CardTitle className="text-base">示例代码</CardTitle></CardHeader>
                <CardContent className="space-y-4">
                    {[
                        {name: 'Bash / curl', code: curlDemo},
                        {name: 'Python', code: pyDemo},
                        {name: 'Node.js', code: jsDemo},
                    ].map((s) => (
                        <div key={s.name}>
                            <div className="flex items-center justify-between mb-1.5">
                                <span className="text-sm font-medium text-gray-700">{s.name}</span>
                                <Button size="sm" variant="ghost" onClick={() => copy(s.code)}>
                                    <Copy className="w-3.5 h-3.5 mr-1"/>复制
                                </Button>
                            </div>
                            <pre className="bg-gray-900 text-gray-100 text-xs p-3 rounded-lg overflow-x-auto">
{s.code}
                            </pre>
                        </div>
                    ))}
                </CardContent>
            </Card>
        </div>
    );
}
