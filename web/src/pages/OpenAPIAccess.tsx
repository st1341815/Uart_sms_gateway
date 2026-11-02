import {useState} from 'react';
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query';
import {toast} from 'sonner';
import {
    AlertTriangle,
    Check,
    Code2,
    Copy,
    FileText,
    History,
    KeyRound,
    Loader2,
    Plus,
    RotateCw,
    Trash2,
} from 'lucide-react';
import {Button} from '@/components/ui/button';
import {Input} from '@/components/ui/input';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {PageHeader} from '@/components/PageHeader';
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
import {cn} from '@/lib/utils';

function copy(text: string) {
    navigator.clipboard?.writeText(text).then(
        () => toast.success('已复制'),
        () => toast.error('复制失败，请手动选择'),
    );
}

const STATUS_TONE: Record<string, string> = {
    accepted: 'bg-emerald-50 text-emerald-700 border-emerald-200',
    rejected: 'bg-rose-50 text-rose-700 border-rose-200',
    failed: 'bg-slate-100 text-slate-600 border-slate-200',
};

const STATUS_TEXT: Record<string, string> = {
    accepted: '已接收',
    rejected: '已拒绝',
    failed: '失败',
};

export default function OpenAPIAccess() {
    const qc = useQueryClient();
    const [tab, setTab] = useState<'keys' | 'templates' | 'docs' | 'logs'>('keys');
    const [newSecret, setNewSecret] = useState<{accessKey: string; secret: string} | null>(null);

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
    const logs = logsResp?.logs ?? [];

    // ---------- Key ----------
    const [keyForm, setKeyForm] = useState({name: '', deviceId: '', allowedIps: '', rateLimitPerMinute: 20});
    const [showKeyForm, setShowKeyForm] = useState(false);

    const createKeyMut = useMutation({
        mutationFn: createKey,
        onSuccess: (res) => {
            setNewSecret({accessKey: res.key.accessKey, secret: res.secret});
            setShowKeyForm(false);
            setKeyForm({name: '', deviceId: '', allowedIps: '', rateLimitPerMinute: 20});
            qc.invalidateQueries({queryKey: ['openapi-keys']});
            toast.success('已创建，请立即保存 Secret');
        },
        onError: (e: unknown) => toast.error(e instanceof Error ? e.message : '创建失败'),
    });

    const toggleKeyMut = useMutation({
        mutationFn: (k: APIKey) =>
            updateKey(k.id, {
                name: k.name,
                deviceId: k.deviceId,
                allowedIps: k.allowedIps,
                rateLimitPerMinute: k.rateLimitPerMinute,
                enabled: !k.enabled,
            }),
        onSuccess: () => {
            qc.invalidateQueries({queryKey: ['openapi-keys']});
            toast.success('已更新');
        },
        onError: (e: unknown) => toast.error(e instanceof Error ? e.message : '更新失败'),
    });

    const deleteKeyMut = useMutation({
        mutationFn: deleteKey,
        onSuccess: () => {
            qc.invalidateQueries({queryKey: ['openapi-keys']});
            toast.success('已删除');
        },
        onError: (e: unknown) => toast.error(e instanceof Error ? e.message : '删除失败'),
    });

    const resetMut = useMutation({
        mutationFn: resetSecret,
        onSuccess: (res, id) => {
            const k = keys.find((x) => x.id === id);
            setNewSecret({accessKey: k?.accessKey || '', secret: res.secret});
            qc.invalidateQueries({queryKey: ['openapi-keys']});
            toast.success('Secret 已重置');
        },
        onError: (e: unknown) => toast.error(e instanceof Error ? e.message : '重置失败'),
    });

    // ---------- 模板 ----------
    const [tplForm, setTplForm] = useState({id: '', code: '', name: '', content: '', description: '', enabled: true});
    const [showTplForm, setShowTplForm] = useState(false);

    const saveTplMut = useMutation({
        mutationFn: async () => {
            const req = {
                code: tplForm.code,
                name: tplForm.name,
                content: tplForm.content,
                description: tplForm.description,
                enabled: tplForm.enabled,
            };
            return tplForm.id ? updateTemplate(tplForm.id, req) : createTemplate(req);
        },
        onSuccess: () => {
            qc.invalidateQueries({queryKey: ['openapi-templates']});
            setShowTplForm(false);
            setTplForm({id: '', code: '', name: '', content: '', description: '', enabled: true});
            toast.success('已保存');
        },
        onError: (e: unknown) => toast.error(e instanceof Error ? e.message : '保存失败'),
    });

    const deleteTplMut = useMutation({
        mutationFn: deleteTemplate,
        onSuccess: () => {
            qc.invalidateQueries({queryKey: ['openapi-templates']});
            toast.success('已删除');
        },
        onError: (e: unknown) => toast.error(e instanceof Error ? e.message : '删除失败'),
    });

    const tabs = [
        {key: 'keys', label: 'API 密钥', icon: KeyRound},
        {key: 'templates', label: '短信模板', icon: FileText},
        {key: 'docs', label: '接入文档', icon: Code2},
        {key: 'logs', label: '调用记录', icon: History},
    ] as const;

    const inputClass = 'bg-slate-50 border-slate-200 focus-visible:border-blue-500 focus-visible:bg-white text-sm';

    return (
        <div className="space-y-6">
            <PageHeader
                title="开放接口"
                description="其他应用可通过 AK/SK 签名调用本接口发送短信验证码。"
                action={
                    <span className="rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs text-slate-500">
                        {keys.length} 个密钥 · {templates.length} 个模板
                    </span>
                }
            />

            {/* 一次性 Secret */}
            {newSecret && (
                <div className="rounded-2xl border border-amber-300 bg-amber-50 p-4">
                    <div className="flex items-center gap-2 text-sm font-bold text-amber-900">
                        <AlertTriangle className="size-4"/>
                        Secret 只显示这一次，请立即保存
                    </div>
                    <div className="mt-3 grid gap-2">
                        {[
                            ['AccessKey', newSecret.accessKey],
                            ['SecretKey', newSecret.secret],
                        ].map(([label, value]) => (
                            <div key={label} className="flex items-center gap-2">
                                <span className="w-24 shrink-0 text-xs font-medium text-amber-800">{label}</span>
                                <code className="min-w-0 flex-1 break-all rounded-lg border border-amber-200 bg-white px-3 py-2 font-mono text-xs text-slate-800">
                                    {value}
                                </code>
                                <Button size="sm" variant="outline" onClick={() => copy(value)} className="shrink-0">
                                    <Copy className="size-3.5"/>
                                </Button>
                            </div>
                        ))}
                    </div>
                    <Button size="sm" variant="ghost" onClick={() => setNewSecret(null)} className="mt-2 text-amber-800 hover:bg-amber-100">
                        <Check className="size-3.5"/>
                        我已保存，关闭
                    </Button>
                </div>
            )}

            {/* Tabs */}
            <div className="flex gap-1 border-b border-slate-200">
                {tabs.map((item) => {
                    const Icon = item.icon;
                    const active = tab === item.key;
                    return (
                        <button
                            key={item.key}
                            type="button"
                            onClick={() => setTab(item.key)}
                            className={cn(
                                '-mb-px flex items-center gap-1.5 border-b-2 px-4 py-2.5 text-sm font-semibold transition-colors',
                                active
                                    ? 'border-blue-600 text-blue-700'
                                    : 'border-transparent text-slate-500 hover:border-slate-300 hover:text-slate-700',
                            )}
                        >
                            <Icon className="size-4"/>
                            {item.label}
                        </button>
                    );
                })}
            </div>

            {/* ===== 密钥 ===== */}
            {tab === 'keys' && (
                <div className="space-y-4">
                    <div className="flex justify-end">
                        <Button size="sm" onClick={() => setShowKeyForm(!showKeyForm)} className="bg-blue-600 text-white hover:bg-blue-700">
                            <Plus className="size-4"/>
                            新建密钥
                        </Button>
                    </div>

                    {showKeyForm && (
                        <Card className="gap-0 py-0">
                            <CardContent className="grid gap-4 py-5 sm:grid-cols-2">
                                <div className="space-y-1.5">
                                    <label className="block text-xs font-semibold text-slate-700">名称 <span className="text-rose-500">*</span></label>
                                    <Input
                                        value={keyForm.name}
                                        onChange={(e) => setKeyForm({...keyForm, name: e.target.value})}
                                        placeholder="如：HVAccRM 验证码"
                                        className={inputClass}
                                    />
                                </div>
                                <div className="space-y-1.5">
                                    <label className="block text-xs font-semibold text-slate-700">发送设备 <span className="text-rose-500">*</span></label>
                                    <select
                                        value={keyForm.deviceId}
                                        onChange={(e) => setKeyForm({...keyForm, deviceId: e.target.value})}
                                        className="h-9 w-full rounded-md border border-slate-200 bg-slate-50 px-3 text-sm outline-none focus:border-blue-500 focus:bg-white"
                                    >
                                        <option value="">请选择设备</option>
                                        {devices.map((d) => (
                                            <option key={d.id} value={d.id}>
                                                {d.name || d.id}（{d.id}）
                                            </option>
                                        ))}
                                    </select>
                                </div>
                                <div className="space-y-1.5">
                                    <label className="block text-xs font-semibold text-slate-700">IP 白名单</label>
                                    <Input
                                        value={keyForm.allowedIps}
                                        onChange={(e) => setKeyForm({...keyForm, allowedIps: e.target.value})}
                                        placeholder="192.168.11.0/24, 1.2.3.4（留空不限制）"
                                        className={inputClass}
                                    />
                                </div>
                                <div className="space-y-1.5">
                                    <label className="block text-xs font-semibold text-slate-700">每分钟调用上限</label>
                                    <Input
                                        type="number"
                                        value={keyForm.rateLimitPerMinute}
                                        onChange={(e) => setKeyForm({...keyForm, rateLimitPerMinute: parseInt(e.target.value) || 20})}
                                        className={inputClass}
                                    />
                                </div>
                                <div className="flex gap-2 sm:col-span-2">
                                    <Button size="sm" disabled={createKeyMut.isPending} onClick={() => createKeyMut.mutate(keyForm)} className="bg-blue-600 text-white hover:bg-blue-700">
                                        {createKeyMut.isPending && <Loader2 className="size-3.5 animate-spin"/>}
                                        {createKeyMut.isPending ? '创建中...' : '创建'}
                                    </Button>
                                    <Button size="sm" variant="outline" onClick={() => setShowKeyForm(false)}>取消</Button>
                                </div>
                            </CardContent>
                        </Card>
                    )}

                    {keysLoading ? (
                        <div className="flex items-center justify-center rounded-2xl border border-slate-200 bg-white py-16 text-sm text-slate-500">
                            <Loader2 className="mr-2 size-4 animate-spin text-blue-600"/>
                            正在读取密钥
                        </div>
                    ) : keys.length === 0 ? (
                        <div className="flex flex-col items-center rounded-2xl border border-slate-200 bg-white px-6 py-16 text-center">
                            <span className="flex size-14 items-center justify-center rounded-full bg-blue-50 text-blue-600">
                                <KeyRound className="size-7"/>
                            </span>
                            <p className="mt-4 text-sm font-semibold text-slate-700">还没有密钥</p>
                            <p className="mt-1 text-xs text-slate-400">点击「新建密钥」开始接入</p>
                        </div>
                    ) : (
                        <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white">
                            {keys.map((k, index) => (
                                <div
                                    key={k.id}
                                    className={cn(
                                        'flex flex-wrap items-center gap-3 px-5 py-4',
                                        index > 0 && 'border-t border-slate-100',
                                    )}
                                >
                                    <div className="min-w-[220px] flex-1">
                                        <div className="flex items-center gap-2">
                                            <span className="text-sm font-bold text-slate-900">{k.name}</span>
                                            <span className={cn(
                                                'rounded-full border px-2 py-0.5 text-[10px] font-semibold',
                                                k.enabled
                                                    ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                                                    : 'border-slate-200 bg-slate-100 text-slate-500',
                                            )}>
                                                {k.enabled ? '启用' : '停用'}
                                            </span>
                                        </div>
                                        <div className="mt-1 break-all font-mono text-xs text-slate-600">
                                            {k.accessKey} · {k.secretHint}
                                        </div>
                                        <div className="mt-1 text-[11px] text-slate-400">
                                            设备 {k.deviceId} · {k.rateLimitPerMinute} 次/分
                                            {k.allowedIps ? ` · IP: ${k.allowedIps}` : ' · 不限 IP'}
                                            {k.lastUsedAt > 0 && ` · 最近 ${new Date(k.lastUsedAt).toLocaleString('zh-CN')}`}
                                        </div>
                                    </div>
                                    <div className="flex shrink-0 gap-1.5">
                                        <Button size="sm" variant="outline" onClick={() => toggleKeyMut.mutate(k)}>
                                            {k.enabled ? '停用' : '启用'}
                                        </Button>
                                        <Button
                                            size="sm"
                                            variant="outline"
                                            onClick={() => {
                                                if (confirm(`重置「${k.name}」的 Secret？旧 Secret 将立即失效。`)) resetMut.mutate(k.id);
                                            }}
                                        >
                                            <RotateCw className="size-3.5"/>
                                            重置 Secret
                                        </Button>
                                        <Button
                                            size="sm"
                                            variant="ghost"
                                            className="text-rose-600 hover:bg-rose-50 hover:text-rose-700"
                                            onClick={() => {
                                                if (confirm(`删除「${k.name}」？该操作不可恢复。`)) deleteKeyMut.mutate(k.id);
                                            }}
                                        >
                                            <Trash2 className="size-4"/>
                                        </Button>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            )}

            {/* ===== 模板 ===== */}
            {tab === 'templates' && (
                <div className="space-y-4">
                    <div className="flex justify-end">
                        <Button
                            size="sm"
                            className="bg-blue-600 text-white hover:bg-blue-700"
                            onClick={() => {
                                setTplForm({id: '', code: '', name: '', content: '', description: '', enabled: true});
                                setShowTplForm(true);
                            }}
                        >
                            <Plus className="size-4"/>
                            新建模板
                        </Button>
                    </div>

                    {showTplForm && (
                        <Card className="gap-0 py-0">
                            <CardContent className="grid gap-4 py-5 sm:grid-cols-2">
                                <div className="space-y-1.5">
                                    <label className="block text-xs font-semibold text-slate-700">模板编码 <span className="text-rose-500">*</span></label>
                                    <Input
                                        value={tplForm.code}
                                        placeholder="verify_code"
                                        onChange={(e) => setTplForm({...tplForm, code: e.target.value})}
                                        disabled={!!tplForm.id}
                                        className={cn(inputClass, 'font-mono')}
                                    />
                                    <p className="text-[11px] text-slate-400">调用接口时传这个编码，创建后不可改</p>
                                </div>
                                <div className="space-y-1.5">
                                    <label className="block text-xs font-semibold text-slate-700">模板名称 <span className="text-rose-500">*</span></label>
                                    <Input
                                        value={tplForm.name}
                                        placeholder="登录验证码"
                                        onChange={(e) => setTplForm({...tplForm, name: e.target.value})}
                                        className={inputClass}
                                    />
                                </div>
                                <div className="space-y-1.5 sm:col-span-2">
                                    <label className="block text-xs font-semibold text-slate-700">
                                        模板内容 <span className="text-rose-500">*</span>
                                        <span className="ml-2 font-normal text-slate-400">变量用 {'{code}'} 形式</span>
                                    </label>
                                    <textarea
                                        value={tplForm.content}
                                        onChange={(e) => setTplForm({...tplForm, content: e.target.value})}
                                        rows={3}
                                        placeholder="您的验证码是{code}，5分钟内有效，请勿泄露。"
                                        className="w-full resize-none rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-sm outline-none focus:border-blue-500 focus:bg-white"
                                    />
                                </div>
                                <div className="space-y-1.5 sm:col-span-2">
                                    <label className="block text-xs font-semibold text-slate-700">说明</label>
                                    <Input
                                        value={tplForm.description}
                                        onChange={(e) => setTplForm({...tplForm, description: e.target.value})}
                                        className={inputClass}
                                    />
                                </div>
                                <label className="flex items-center gap-2 text-sm text-slate-700 sm:col-span-2">
                                    <input
                                        type="checkbox"
                                        checked={tplForm.enabled}
                                        onChange={(e) => setTplForm({...tplForm, enabled: e.target.checked})}
                                        className="size-4 rounded border-slate-300"
                                    />
                                    启用此模板
                                </label>
                                <div className="flex gap-2 sm:col-span-2">
                                    <Button size="sm" disabled={saveTplMut.isPending} onClick={() => saveTplMut.mutate()} className="bg-blue-600 text-white hover:bg-blue-700">
                                        {saveTplMut.isPending && <Loader2 className="size-3.5 animate-spin"/>}
                                        {saveTplMut.isPending ? '保存中...' : '保存'}
                                    </Button>
                                    <Button size="sm" variant="outline" onClick={() => setShowTplForm(false)}>取消</Button>
                                </div>
                            </CardContent>
                        </Card>
                    )}

                    {templates.length === 0 ? (
                        <div className="flex flex-col items-center rounded-2xl border border-slate-200 bg-white px-6 py-16 text-center">
                            <span className="flex size-14 items-center justify-center rounded-full bg-blue-50 text-blue-600">
                                <FileText className="size-7"/>
                            </span>
                            <p className="mt-4 text-sm font-semibold text-slate-700">还没有模板</p>
                            <p className="mt-1 text-xs text-slate-400">创建模板后，接口即可按编码发送</p>
                        </div>
                    ) : (
                        <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white">
                            {templates.map((t, index) => (
                                <div key={t.id} className={cn('px-5 py-4', index > 0 && 'border-t border-slate-100')}>
                                    <div className="flex items-start justify-between gap-3">
                                        <div className="min-w-0 flex-1">
                                            <div className="flex flex-wrap items-center gap-2">
                                                <span className="text-sm font-bold text-slate-900">{t.name}</span>
                                                <code className="rounded bg-blue-50 px-1.5 py-0.5 font-mono text-xs text-blue-700">{t.code}</code>
                                                {!t.enabled && (
                                                    <span className="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] text-slate-500">已停用</span>
                                                )}
                                            </div>
                                            <p className="mt-1.5 break-words text-xs text-slate-600">{t.content}</p>
                                            {t.params.length > 0 && (
                                                <p className="mt-1 font-mono text-[11px] text-slate-400">
                                                    变量：{t.params.map((p) => `{${p}}`).join(' ')}
                                                </p>
                                            )}
                                        </div>
                                        <div className="flex shrink-0 gap-1.5">
                                            <Button
                                                size="sm"
                                                variant="outline"
                                                onClick={() => {
                                                    setTplForm({
                                                        id: t.id,
                                                        code: t.code,
                                                        name: t.name,
                                                        content: t.content,
                                                        description: t.description,
                                                        enabled: t.enabled,
                                                    });
                                                    setShowTplForm(true);
                                                }}
                                            >
                                                编辑
                                            </Button>
                                            <Button
                                                size="sm"
                                                variant="ghost"
                                                className="text-rose-600 hover:bg-rose-50 hover:text-rose-700"
                                                onClick={() => {
                                                    if (confirm(`删除模板「${t.name}」？`)) deleteTplMut.mutate(t.id);
                                                }}
                                            >
                                                <Trash2 className="size-4"/>
                                            </Button>
                                        </div>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            )}

            {/* ===== 文档 ===== */}
            {tab === 'docs' && <DocsPanel keys={keys}/>}

            {/* ===== 日志 ===== */}
            {tab === 'logs' && (
                <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white">
                    {logs.length === 0 ? (
                        <div className="flex flex-col items-center px-6 py-16 text-center">
                            <span className="flex size-14 items-center justify-center rounded-full bg-slate-100 text-slate-400">
                                <History className="size-7"/>
                            </span>
                            <p className="mt-4 text-sm font-semibold text-slate-700">暂无调用记录</p>
                            <p className="mt-1 text-xs text-slate-400">外部应用成功调用后会出现在这里（每 5 秒刷新）</p>
                        </div>
                    ) : (
                        <div className="overflow-x-auto">
                            <table className="w-full text-xs">
                                <thead>
                                    <tr className="border-b border-slate-200 bg-slate-50 text-[11px] font-semibold text-slate-500">
                                        {['时间', '密钥', '手机号', '模板', '状态', '来源 IP', '原因'].map((h) => (
                                            <th key={h} className="whitespace-nowrap px-3 py-2.5 text-left">
                                                {h}
                                            </th>
                                        ))}
                                    </tr>
                                </thead>
                                <tbody>
                                    {logs.map((l) => (
                                        <tr key={l.id} className={cn('border-b border-slate-100 last:border-b-0', l.status === 'rejected' && 'bg-rose-50/40')}>
                                            <td className="whitespace-nowrap px-3 py-2.5 text-slate-500">
                                                {new Date(l.createdAt).toLocaleString('zh-CN')}
                                            </td>
                                            <td className="whitespace-nowrap px-3 py-2.5 font-mono text-slate-600">{l.accessKey?.slice(0, 12)}…</td>
                                            <td className="whitespace-nowrap px-3 py-2.5 font-mono">{l.phone || '—'}</td>
                                            <td className="whitespace-nowrap px-3 py-2.5">{l.templateId || '—'}</td>
                                            <td className="px-3 py-2.5">
                                                <span className={cn(
                                                    'rounded border px-1.5 py-0.5 text-[10px] font-semibold',
                                                    STATUS_TONE[l.status] ?? 'border-slate-200 bg-slate-100 text-slate-600',
                                                )}>
                                                    {STATUS_TEXT[l.status] ?? l.status}
                                                </span>
                                            </td>
                                            <td className="whitespace-nowrap px-3 py-2.5 font-mono text-slate-500">{l.clientIp}</td>
                                            <td className="px-3 py-2.5 text-slate-600">{l.reason || '—'}</td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}

// ---------- 接入文档 ----------
function DocsPanel({keys}: {keys: APIKey[]}) {
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

    const codeBlock = 'bg-slate-900 text-slate-100 text-xs p-3.5 rounded-xl overflow-x-auto leading-relaxed';

    return (
        <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(300px,0.5fr)]">
            <div className="grid gap-4">
                <Card className="gap-0 py-0">
                    <CardHeader className="border-b border-slate-100 py-4">
                        <CardTitle className="text-sm">调用方式</CardTitle>
                        <p className="mt-1 text-xs text-slate-500">
                            接口地址 <code className="rounded bg-slate-100 px-1.5 py-0.5 font-mono">POST /openapi/v1/sms/send</code>
                        </p>
                    </CardHeader>
                    <CardContent className="space-y-5 py-5 text-sm text-slate-700">
                        <section>
                            <h4 className="mb-2 text-xs font-bold text-slate-800">请求头</h4>
                            <ul className="space-y-1.5 text-xs text-slate-600">
                                {[
                                    ['X-Api-Key', '上面创建的 AccessKey'],
                                    ['X-Timestamp', 'Unix 秒级时间戳，与服务器偏差不能超过 5 分钟'],
                                    ['X-Nonce', '随机串，每次请求必须唯一（防重放）'],
                                    ['X-Signature', 'HMAC-SHA256 十六进制小写'],
                                ].map(([k, v]) => (
                                    <li key={k} className="flex gap-2">
                                        <code className="w-28 shrink-0 font-mono text-blue-700">{k}</code>
                                        <span>{v}</span>
                                    </li>
                                ))}
                            </ul>
                        </section>

                        <section>
                            <h4 className="mb-2 text-xs font-bold text-slate-800">签名算法</h4>
                            <pre className={codeBlock}>{`StringToSign =
  METHOD + "\\n" +
  PATH + "\\n" +
  SORTED_QUERY + "\\n" +
  SHA256_HEX(body) + "\\n" +
  TIMESTAMP + "\\n" +
  NONCE

Signature = HMAC-SHA256(SecretKey, StringToSign)  →  hex 小写`}</pre>
                            <p className="mt-2 text-[11px] leading-5 text-slate-500">
                                SORTED_QUERY：query 参数按 key 升序拼成 <code className="rounded bg-slate-100 px-1">a=1&amp;b=2</code>，
                                无参数则为空字符串。PATH 不含域名与 query。
                            </p>
                        </section>

                        <section>
                            <h4 className="mb-2 text-xs font-bold text-slate-800">请求体</h4>
                            <pre className="overflow-x-auto rounded-xl border border-slate-200 bg-slate-50 p-3.5 text-xs leading-relaxed">{`{
  "phone": "13800138000",
  "template": "verify_code",
  "params": { "code": "123456" }
}
// 或不走模板：{ "phone": "...", "content": "完整内容" }`}</pre>
                        </section>

                        <section>
                            <h4 className="mb-2 text-xs font-bold text-slate-800">响应</h4>
                            <pre className="overflow-x-auto rounded-xl border border-slate-200 bg-slate-50 p-3.5 text-xs leading-relaxed">{`// 成功
{ "success": true, "code": "OK", "message": "短信已提交发送",
  "data": { "messageId": "...", "phone": "138****8000",
            "deviceId": "sms1", "status": "accepted" } }

// 失败
{ "success": false, "code": "PHONE_TOO_FREQUENT",
  "message": "该手机号发送过于频繁，请 59 秒后重试" }`}</pre>
                        </section>
                    </CardContent>
                </Card>

                <Card className="gap-0 py-0">
                    <CardHeader className="border-b border-slate-100 py-4">
                        <CardTitle className="text-sm">示例代码</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-4 py-5">
                        {[
                            {name: 'Bash / curl', code: curlDemo},
                            {name: 'Python', code: pyDemo},
                            {name: 'Node.js', code: jsDemo},
                        ].map((s) => (
                            <div key={s.name}>
                                <div className="mb-1.5 flex items-center justify-between">
                                    <span className="text-xs font-semibold text-slate-700">{s.name}</span>
                                    <Button size="sm" variant="ghost" onClick={() => copy(s.code)}>
                                        <Copy className="size-3.5"/>
                                        复制
                                    </Button>
                                </div>
                                <pre className={codeBlock}>{s.code}</pre>
                            </div>
                        ))}
                    </CardContent>
                </Card>
            </div>

            <Card className="gap-0 py-0 xl:sticky xl:top-[84px]">
                <CardHeader className="border-b border-slate-100 py-4">
                    <CardTitle className="text-sm">错误码</CardTitle>
                    <p className="mt-1 text-xs text-slate-500">排查失败原因时对照</p>
                </CardHeader>
                <CardContent className="divide-y divide-slate-100 py-1">
                    {[
                        ['UNAUTHORIZED', '401', '签名 / 时间戳 / nonce 校验失败'],
                        ['INVALID_PHONE', '400', '手机号为空或格式错误'],
                        ['TEMPLATE_NOT_FOUND', '400', '模板不存在或已停用'],
                        ['TEMPLATE_PARAM_MISSING', '400', '缺少模板变量'],
                        ['PHONE_TOO_FREQUENT', '429', '同号码 60 秒内重复发送'],
                        ['PHONE_DAILY_LIMIT', '429', '同号码 24 小时超过 10 条'],
                        ['KEY_RATE_LIMIT', '429', '该 Key 超过每分钟上限'],
                        ['DEVICE_UNAVAILABLE', '500', '绑定设备不可用'],
                    ].map(([code, http, desc]) => (
                        <div key={code} className="py-2.5">
                            <div className="flex items-center gap-2">
                                <code className="font-mono text-[11px] font-semibold text-rose-600">{code}</code>
                                <span className="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-semibold text-slate-500">{http}</span>
                            </div>
                            <p className="mt-1 text-[11px] leading-5 text-slate-500">{desc}</p>
                        </div>
                    ))}
                </CardContent>
            </Card>
        </div>
    );
}
