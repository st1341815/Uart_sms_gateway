import {useState} from 'react';
import {useQuery} from '@tanstack/react-query';
import {
    AlertTriangle,
    CheckCircle2,
    ChevronDown,
    ChevronUp,
    Copy,
    Info,
    Loader2,
    PackagePlus,
    Radio,
    RefreshCw,
    ShieldCheck,
    XCircle,
} from 'lucide-react';
import {toast} from 'sonner';
import * as selfCheckApi from '../api/selfcheck';
import type {SelfCheckItem, SelfCheckModule} from '../api/selfcheck';
import {Button} from '@/components/ui/button';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {cn} from '@/lib/utils';

const LEVEL_STYLE = {
    error: {Icon: XCircle, cls: 'text-err'},
    warn: {Icon: AlertTriangle, cls: 'text-warn'},
    info: {Icon: Info, cls: 'text-fg-3'},
    ok: {Icon: CheckCircle2, cls: 'text-ok'},
} as const;

// 复制到剪贴板。
//
// ⚠️ 内网是 http:// 访问的（非安全上下文），navigator.clipboard 在那里直接不可用，
// 所以必须有 execCommand 兜底，否则这个功能在用户真实环境里是死的。
const copyText = async (text: string, label: string) => {
    try {
        if (navigator.clipboard && window.isSecureContext) {
            await navigator.clipboard.writeText(text);
        } else {
            const ta = document.createElement('textarea');
            ta.value = text;
            ta.style.position = 'fixed';
            ta.style.top = '-1000px';
            ta.style.opacity = '0';
            document.body.appendChild(ta);
            ta.select();
            document.execCommand('copy');
            document.body.removeChild(ta);
        }
        toast.success(`${label}已复制`);
    } catch {
        toast.error('复制失败，请展开后手动选中复制');
    }
};

function ItemRow({item}: {item: SelfCheckItem}) {
    const {Icon, cls} = LEVEL_STYLE[item.level] ?? LEVEL_STYLE.info;

    return (
        <div className="flex items-start gap-2.5 rounded-md border border-line-soft bg-surface-2/60 p-3">
            <Icon className={cn('mt-0.5 size-3.5 shrink-0', cls)}/>
            <div className="min-w-0 flex-1">
                <p className="text-xs font-medium leading-5 text-fg-2">{item.message}</p>
                {item.next_step && (
                    <p className="mt-1 text-[11px] leading-5 text-fg-3">→ {item.next_step}</p>
                )}
            </div>
        </div>
    );
}

// 后端错误体是 {"error": "..."}，直接 toast 出去等于把 JSON 甩到用户脸上
function apiErrorText(err: unknown): string {
    const raw = err instanceof Error ? err.message : String(err);
    try {
        const parsed = JSON.parse(raw) as {error?: string};
        if (parsed && typeof parsed.error === 'string') return parsed.error;
    } catch {
        // 不是 JSON，原样返回
    }
    return raw;
}

function UnconfiguredModuleCard({module}: {module: SelfCheckModule}) {
    const [probing, setProbing] = useState(false);
    const [probeResult, setProbeResult] = useState<selfCheckApi.ProbeResult | null>(null);
    const [probeError, setProbeError] = useState<string | null>(null);

    // 探测会短暂占用串口，所以只对「容器内可见」的未配置模组开放；
    // 已配置的端口后端会直接 409，这里连按钮都不给（避免用户误以为能点）。
    const runProbe = async () => {
        setProbing(true);
        setProbeError(null);
        try {
            const result = await selfCheckApi.probeModule(module.usb_port);
            setProbeResult(result);
            if (result.success) {
                const tag = [result.operator, result.number].filter(Boolean).join(' ');
                toast.success(`探测成功：${tag || result.at_port}`);
            } else {
                toast.error('没拿到状态帧，卡在哪一步见下面的试探记录');
            }
        } catch (err) {
            const text = apiErrorText(err);
            setProbeError(text);
            toast.error(text);
        } finally {
            setProbing(false);
        }
    };

    return (
        <div className="rounded-md border border-warn/30 bg-warn/10 p-3">
            <div className="flex items-start gap-2.5">
                <PackagePlus className="mt-0.5 size-3.5 shrink-0 text-warn"/>
                <div className="min-w-0 flex-1">
                    <p className="text-xs font-semibold text-warn">
                        未配置的模组 · {module.usb_port}
                        {module.device ? `（${module.device}）` : ''}
                    </p>
                    <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-fg-3">
                        <span>
                            候选 AT 口{' '}
                            <span className="font-mono text-fg-2">{module.candidate_node || '—'}</span>
                        </span>
                        <span className="text-line">|</span>
                        <span>
                            容器内可见：{' '}
                            <span className={module.in_container ? 'text-ok' : 'text-err'}>
                                {module.in_container ? '是' : '否'}
                            </span>
                        </span>
                    </p>
                    {!module.at_confirmed && (
                        <p className="mt-1 text-[11px] text-fg-3">
                            该口不是按 :1.6 规律识别的，需人工确认哪个才是 AT 口。
                        </p>
                    )}
                </div>
            </div>

            <div className="mt-2.5 flex flex-wrap gap-2">
                <Button
                    variant="outline"
                    size="sm"
                    disabled={!module.compose_snippet}
                    onClick={() => copyText(module.compose_snippet ?? '', 'compose 片段')}
                >
                    <Copy className="size-3"/>
                    复制 compose devices
                </Button>
                <Button
                    variant="outline"
                    size="sm"
                    disabled={!module.config_snippet}
                    onClick={() => copyText(module.config_snippet ?? '', 'config 片段')}
                >
                    <Copy className="size-3"/>
                    复制 config Devices
                </Button>
                <Button
                    variant="outline"
                    size="sm"
                    disabled={probing || !module.in_container}
                    onClick={runProbe}
                >
                    {probing
                        ? <Loader2 className="size-3 animate-spin"/>
                        : <Radio className="size-3"/>}
                    {probing ? '正在探测' : '探测 AT 口'}
                </Button>
            </div>

            {!module.in_container && (
                <p className="mt-1.5 text-[11px] leading-5 text-fg-3">
                    容器内看不到这个模组的节点，探测无法进行：先按上面的片段补 compose devices，重建容器后再探。
                </p>
            )}

            {probeError && (
                <div className="mt-2 rounded-md border border-err/30 bg-err/10 p-2.5 text-[11px] leading-5 text-err">
                    探测被拒绝：{probeError}
                </div>
            )}

            {probeResult && (
                <div
                    className={cn(
                        'mt-2 rounded-md border p-2.5',
                        probeResult.success ? 'border-ok/30 bg-ok/10' : 'border-warn/30 bg-warn/10',
                    )}
                >
                    <p className={cn('text-[11px] font-medium leading-5', probeResult.success ? 'text-ok' : 'text-warn')}>
                        {probeResult.message}
                    </p>
                    {probeResult.next_step && (
                        <p className="mt-1 text-[10px] leading-5 text-fg-3">→ {probeResult.next_step}</p>
                    )}

                    {probeResult.success && (
                        <p className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[10px] text-fg-3">
                            <span>ICCID <span className="font-mono text-fg-2">{probeResult.iccid || '—'}</span></span>
                            <span>信号 <span className="font-mono text-fg-2">{probeResult.signal_desc || probeResult.rsrp || '—'}</span></span>
                            <span>
                                识别方式{' '}
                                <span className={probeResult.at_confirmed ? 'text-ok' : 'text-warn'}>
                                    {probeResult.at_confirmed ? '按 :1.6 规律' : '实测试探'}
                                </span>
                            </span>
                        </p>
                    )}

                    <div className="mt-1.5 space-y-0.5">
                        {probeResult.tried.map((attempt) => (
                            <p key={attempt.by_path} className="font-mono text-[10px] leading-4 text-fg-3">
                                {attempt.if_key} · {attempt.node} → {attempt.got_status
                                    ? '命中 status_response'
                                    : (attempt.error ?? '无响应')}
                            </p>
                        ))}
                    </div>

                    {probeResult.success && probeResult.config_snippet && (
                        <div className="mt-2">
                            <Button
                                variant="outline"
                                size="sm"
                                onClick={() => copyText(probeResult.config_snippet ?? '', '探测片段')}
                            >
                                <Copy className="size-3"/>
                                复制探测到的 config 片段
                            </Button>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}

export default function DeviceSelfCheck() {
    const [showAll, setShowAll] = useState(false);

    const {data, isLoading, isFetching, refetch, error} = useQuery({
        queryKey: ['deviceSelfCheck'],
        queryFn: selfCheckApi.getSelfCheck,
        // 60 秒扫一次：只读目录，几乎没有开销；新插的卡能自动冒出来
        refetchInterval: 60000,
        staleTime: 30000,
        retry: false,
    });

    const items = data?.items ?? [];
    // MODULE_UNCONFIGURED 已在下面的模块卡片里单独展示，这里不重复列出
    const problems = items.filter(
        (item) => (item.level === 'error' || item.level === 'warn') && item.code !== 'MODULE_UNCONFIGURED',
    );
    const rest = items.filter((item) => item.level !== 'error' && item.level !== 'warn');
    const unconfigured = (data?.modules ?? []).filter((module) => !module.configured);
    const pending = problems.length + unconfigured.length;
    const healthy = data ? pending === 0 : false;

    const checkedAt = data?.generated_at ? new Date(data.generated_at) : null;

    return (
        <Card className="mt-4 gap-0 py-0">
            <CardHeader className="border-b border-line-soft py-4">
                <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                        <CardTitle className="flex items-center gap-2 text-sm">
                            <ShieldCheck className="size-4 text-brand"/>
                            设备自检
                            {data && (
                                <span className={cn(
                                    'rounded-full border px-2 py-0.5 text-[10px] font-semibold',
                                    healthy
                                        ? 'border-ok/30 bg-ok/10 text-ok'
                                        : 'border-warn/30 bg-warn/10 text-warn',
                                )}>
                                    {healthy ? '全部正常' : `${pending} 项待处理`}
                                </span>
                            )}
                        </CardTitle>
                        <p className="mt-1 text-[11px] leading-5 text-fg-3">
                            只读比对宿主机串口与 config.yaml，不打开串口，不会打扰正在收发的卡
                        </p>
                    </div>
                    <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => refetch()}
                        disabled={isFetching}
                        className="shrink-0"
                    >
                        <RefreshCw className={cn('size-3', isFetching && 'animate-spin')}/>
                        重新自检
                    </Button>
                </div>
            </CardHeader>

            <CardContent className="p-4">
                {isLoading ? (
                    <div className="flex items-center justify-center gap-2 py-6 text-xs text-fg-3">
                        <Loader2 className="size-3.5 animate-spin text-brand"/>
                        正在检查串口与配置
                    </div>
                ) : error ? (
                    <div className="rounded-md border border-err/30 bg-err/10 p-3 text-xs leading-5 text-err">
                        自检接口调用失败：{error instanceof Error ? error.message : String(error)}
                    </div>
                ) : data ? (
                    <div className="space-y-3">
                        {!data.by_path_available && (
                            <div className="rounded-md border border-err/30 bg-err/10 p-3 text-xs leading-5 text-err">
                                容器内读不到 <span className="font-mono">/dev/serial/by-path</span>，
                                只能做「配置 ↔ 容器内节点」的比对，看不到新插入的模组。
                                <p className="mt-1 text-[11px]">
                                    → 确认 docker-compose.yml 里有
                                    <span className="font-mono"> volumes: - /dev/serial:/dev/serial:ro</span>，
                                    然后重建容器。
                                </p>
                            </div>
                        )}

                        {healthy && (
                            <div className="flex items-center gap-2 rounded-md border border-ok/25 bg-ok/10 p-3 text-xs text-ok">
                                <CheckCircle2 className="size-3.5"/>
                                {data.devices.length} 台已配置设备全部正常，也没有发现未配置的模组。
                            </div>
                        )}

                        {unconfigured.length > 0 && (
                            <div className="space-y-2">
                                {unconfigured.map((module) => (
                                    <UnconfiguredModuleCard key={module.usb_port} module={module}/>
                                ))}
                            </div>
                        )}

                        {problems.length > 0 && (
                            <div className="space-y-2">
                                {problems.map((item, index) => (
                                    <ItemRow key={`${item.code}-${item.device_id ?? item.usb_port ?? index}`} item={item}/>
                                ))}
                            </div>
                        )}

                        {rest.length > 0 && (
                            <div className="space-y-2">
                                {showAll && rest.map((item, index) => (
                                    <ItemRow key={`${item.code}-${index}`} item={item}/>
                                ))}
                                <button
                                    type="button"
                                    onClick={() => setShowAll((prev) => !prev)}
                                    className="inline-flex items-center gap-1 text-[11px] text-fg-3 transition hover:text-brand"
                                >
                                    {showAll ? <ChevronUp className="size-3"/> : <ChevronDown className="size-3"/>}
                                    {showAll ? '收起正常项' : `展开 ${rest.length} 条正常/提示项`}
                                </button>
                            </div>
                        )}

                        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-line-soft pt-2.5 text-[10px] text-fg-3">
                            <span>
                                已配置 {data.devices.length} 台 /
                                模组 {data.modules.length} 个
                            </span>
                            {checkedAt && (
                                    <span className="font-mono">
                                    最近检查 {checkedAt.toLocaleTimeString('zh-CN', {hour12: false})}
                                </span>
                            )}
                            {data.summary.error > 0 && <span className="text-err">错误 {data.summary.error}</span>}
                            {data.summary.warn > 0 && <span className="text-warn">警告 {data.summary.warn}</span>}
                        </div>
                    </div>
                ) : null}
            </CardContent>
        </Card>
    );
}
