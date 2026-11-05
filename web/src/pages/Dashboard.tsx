import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowRight, Send, Smartphone, WifiOff } from 'lucide-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { getStats, getConversations } from '@/api/messages';
import { getAllDevicesStatus, sendSMS } from '@/api/serial';
import type { AllDevicesStatusResponse, Conversation, DeviceStatusSummary, Stats } from '@/api/types';
import { PageHeader } from '@/components/PageHeader';
import { cn } from '@/lib/utils';

/** 信号等级 0-31 → 百分比 */
const signalPercent = (signal?: number) => {
    if (!signal || signal <= 0) return 0;
    return Math.max(0, Math.min(100, Math.round((signal / 31) * 100)));
};

/** 0-31 → 6 段柱里点亮几段 */
const signalBars = (signal?: number) => {
    if (!signal || signal <= 0) return 0;
    return Math.max(1, Math.min(6, Math.round((signal / 31) * 6)));
};

const signalTone = (percent: number) => {
    if (percent >= 65) return 'bg-ok';
    if (percent >= 40) return 'bg-warn';
    return 'bg-err';
};

const formatTime = (timestamp?: number) => {
    if (!timestamp) return '—';
    return new Date(timestamp).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
};

const formatShortDate = (timestamp?: number) => {
    if (!timestamp) return '—';
    const date = new Date(timestamp);
    const now = new Date();
    const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    const time = date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
    if (date.getTime() >= startOfToday) return time;
    if (date.getTime() >= startOfToday - 86400000) return `昨天 ${time}`;
    return `${String(date.getMonth() + 1).padStart(2, '0')}/${String(date.getDate()).padStart(2, '0')} ${time}`;
};

/** 6 段信号柱：空段用 bar-idle，亮段按强度取色 */
function SignalBars({ signal, connected }: { signal?: number; connected: boolean }) {
    const lit = connected ? signalBars(signal) : 0;
    const percent = signalPercent(signal);
    return (
        <div className="flex items-end gap-[2px]" aria-hidden>
            {[5, 8, 11, 14, 17, 20].map((height, index) => {
                const filled = index < lit;
                return (
                    <span
                        key={height}
                        className={cn('block w-[4px] rounded-[1px]', filled ? signalTone(percent) : 'bg-[var(--bar-idle)]')}
                        style={{ height }}
                    />
                );
            })}
        </div>
    );
}

function DeviceRow({
    device,
    lastActivity,
}: {
    device: DeviceStatusSummary;
    lastActivity?: number;
}) {
    const percent = signalPercent(device.signal);
    const offline = !device.connected;
    const dotTone = offline ? 'bg-fg-3' : percent >= 40 ? 'bg-ok' : 'bg-err';
    const signalTone = offline ? 'text-fg-3' : percent >= 40 ? 'text-fg-2' : 'text-err';
    const netLabel = device.flymode ? 'FLY' : device.isRoaming ? 'ROAM' : '4G';

    return (
        <>
            {/*
              手机（<640px）：卡片式两行，靠 flex 自然堆叠。
              之前是「表格压成 3 列」，列宽再怎么调都是把桌面布局硬挤进窄屏，
              中文一长就顶破。这里改成没有固定列的结构，窄到 280px 也不会溢出。
            */}
            <div
                className={cn(
                    'flex flex-col gap-1.5 border-b border-line-soft px-3 py-2.5 last:border-b-0 sm:hidden',
                    offline && 'opacity-70',
                )}
            >
                <div className="flex items-center gap-2">
                    <span className={cn('size-[7px] shrink-0 rounded-full', dotTone)}
                          title={offline ? '离线' : percent >= 40 ? '在线' : '信号弱'}/>
                    <span className="cjk-nowrap min-w-0 truncate text-[13.5px] font-semibold text-fg">{device.name}</span>
                    <span className="num shrink-0 text-[10px] text-fg-3">{netLabel}</span>
                    <span className="num ml-auto shrink-0 text-[11px] text-fg-3">{formatTime(lastActivity)}</span>
                </div>
                <div className="flex items-center gap-2 pl-[15px]">
                    <span className="cjk-nowrap min-w-0 truncate text-[11.5px] text-fg-2">
                        {offline ? '—' : device.operator || '未知运营商'}
                    </span>
                    <span className={cn('num shrink-0 text-[11.5px] tracking-tight', offline ? 'text-fg-3' : 'text-fg')}>
                        {device.phoneNumber || '未补录'}
                    </span>
                    <span className="ml-auto flex shrink-0 items-center gap-1.5">
                        <SignalBars signal={device.signal} connected={device.connected}/>
                        <span className={cn('num text-[11px]', signalTone)}>
                            {offline ? '—' : device.signal ?? '—'}
                        </span>
                    </span>
                </div>
            </div>

            {/* ≥640px：沿用七列表格行 */}
            <div
                className={cn(
                    'hidden h-[52px] grid-cols-[20px_minmax(64px,0.75fr)_minmax(0,1.6fr)_minmax(76px,1fr)_minmax(96px,1.1fr)_minmax(58px,0.7fr)_minmax(64px,0.8fr)] items-center gap-3 border-b border-line-soft px-3.5 last:border-b-0 sm:grid',
                    offline && 'opacity-70',
                )}
            >
                <span className={cn('size-[7px] rounded-full', dotTone)}
                      title={offline ? '离线' : percent >= 40 ? '在线' : '信号弱'}/>

                <span className="num min-w-0 truncate whitespace-nowrap text-[11.5px] text-fg-2"
                      title={device.port || device.id}>
                    {device.port || device.id}
                </span>

                <div className="min-w-0">
                    <div className="flex items-baseline gap-1.5">
                        <span className="cjk-nowrap truncate text-[13.5px] font-semibold text-fg">{device.name}</span>
                        <span className="num text-[10px] text-fg-3">{netLabel}</span>
                    </div>
                </div>

                <span className="whitespace-nowrap text-xs text-fg-2">
                    {offline ? '—' : device.operator || '未知运营商'}
                </span>

                <span className={cn('num whitespace-nowrap text-[12.5px] tracking-tight', offline ? 'text-fg-3' : 'text-fg')}>
                    {device.phoneNumber || '未补录'}
                </span>

                <div className="flex items-center gap-1.5">
                    <SignalBars signal={device.signal} connected={device.connected}/>
                    <span className={cn('num text-[11.5px]', signalTone)}>
                        {offline ? '—' : device.signal ?? '—'}
                    </span>
                </div>

                <span className="num whitespace-nowrap text-right text-[11px] text-fg-3">{formatTime(lastActivity)}</span>
            </div>
        </>
    );
}

export default function Dashboard() {
    const queryClient = useQueryClient();

    const { data: devicesResponse } = useQuery<AllDevicesStatusResponse>({
        queryKey: ['allDevicesStatus'],
        queryFn: getAllDevicesStatus,
        refetchInterval: 10000,
    });

    const { data: stats } = useQuery<Stats>({
        queryKey: ['stats'],
        queryFn: getStats,
        refetchInterval: 30000,
    });

    const { data: conversations = [] } = useQuery<Conversation[]>({
        queryKey: ['conversations'],
        queryFn: getConversations,
        refetchInterval: 30000,
    });

    const devices = devicesResponse?.devices ?? [];
    const onlineDevices = devices.filter((device) => device.connected);
    const totalCount = devices.length;
    const leadDevice = onlineDevices[0];
    const leadPercent = leadDevice ? signalPercent(leadDevice.signal) : 0;

    // 每台设备最后一次活动：从会话的最后一条消息里取最大时间戳（真实数据，不是造的）
    const lastActivityByDevice = useMemo(() => {
        const map = new Map<string, number>();
        for (const conversation of conversations) {
            const deviceId = conversation.deviceId || '';
            const timestamp = conversation.lastMessage?.timestamp ?? 0;
            map.set(deviceId, Math.max(map.get(deviceId) ?? 0, timestamp));
        }
        return map;
    }, [conversations]);

    const unreadList = useMemo(
        () =>
            [...conversations]
                .filter((conversation) => conversation.unreadCount > 0)
                .sort((a, b) => (b.lastMessage?.timestamp ?? 0) - (a.lastMessage?.timestamp ?? 0))
                .slice(0, 4),
        [conversations],
    );

    const recentList = useMemo(
        () =>
            [...conversations]
                .sort((a, b) => (b.lastMessage?.timestamp ?? 0) - (a.lastMessage?.timestamp ?? 0))
                .slice(0, 5),
        [conversations],
    );

    const incoming = stats?.incomingCount ?? 0;
    const outgoing = stats?.outgoingCount ?? 0;
    const flowTotal = incoming + outgoing;
    const incomingShare = flowTotal > 0 ? Math.round((incoming / flowTotal) * 100) : 0;

    // ── 快捷发送 ──
    const [composeDeviceId, setComposeDeviceId] = useState('');
    const [composeTo, setComposeTo] = useState('');
    const [composeText, setComposeText] = useState('');

    const effectiveDeviceId = composeDeviceId || leadDevice?.id || '';
    const effectiveDevice = devices.find((device) => device.id === effectiveDeviceId);

    const sendMutation = useMutation({
        mutationFn: sendSMS,
        onSuccess: () => {
            toast.success('短信已提交发送');
            setComposeText('');
            void queryClient.invalidateQueries({ queryKey: ['conversations'] });
            void queryClient.invalidateQueries({ queryKey: ['stats'] });
        },
        onError: (error: unknown) => {
            const message = error instanceof Error ? error.message : '发送失败';
            toast.error(message);
        },
    });

    const canSend = effectiveDevice?.connected && composeTo.trim() && composeText.trim().length > 0;

    const handleSend = () => {
        if (!canSend) return;
        sendMutation.mutate({
            to: composeTo.trim(),
            content: composeText.trim(),
            device_id: effectiveDeviceId,
        });
    };

    const serviceRows: Array<[string, string, boolean]> = [
        ['串口链路', onlineDevices.length > 0 ? `${onlineDevices.length} 台在线` : '等待', onlineDevices.length > 0],
        ['短信监听', onlineDevices.length > 0 ? '正常' : '等待', onlineDevices.length > 0],
        ['短信入库', '正常', true],
    ];

    return (
        <div className="flex h-full min-h-0 flex-col">
            <PageHeader
                title="设备概览"
                description="多设备短信转发运行时状态：串口链路、蜂窝网络与短信流量"
                action={
                    <div className="flex items-center gap-2">
                        <span className="num hidden text-[11px] text-fg-3 sm:inline">
                            {leadDevice ? `${leadDevice.name} · ${leadDevice.signal ?? '—'}/31 · ${leadPercent}%` : '等待设备接入'}
                        </span>
                        <Link
                            to="/messages"
                            className="inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-3 text-xs font-semibold text-fg-2 transition-colors hover:border-brand/50 hover:text-brand"
                        >
                            <Send className="size-3.5"/>
                            短信中心
                        </Link>
                    </div>
                }
            />

            <div className="mt-4 grid min-h-0 min-w-0 flex-1 gap-3 lg:grid-cols-[minmax(0,1fr)_380px]">
                {/* ── 左：设备矩阵 + 下方面板 ── */}
                <div className="flex min-h-0 min-w-0 flex-col gap-3">
                    <section className="flex min-h-0 flex-col">
                        <div className="mb-2 flex items-baseline gap-2.5">
                            <h2 className="text-[12px] font-semibold uppercase tracking-[0.14em] text-fg-2">设备矩阵</h2>
                            <span className="num text-[11px] text-fg-3">
                                {totalCount > 0 ? `${totalCount} 端口 · by-path 绑定` : '未配置设备'}
                            </span>
                            <div className="flex-1"/>
                            <Link
                                to="/serial"
                                className="inline-flex items-center gap-1 text-[11px] font-semibold text-fg-2 transition-colors hover:text-brand"
                            >
                                串口控制 <ArrowRight className="size-3"/>
                            </Link>
                        </div>

                        <div className="overflow-hidden rounded-md border border-line bg-surface-2">
                            <div className="hidden h-8 grid-cols-[20px_minmax(64px,0.75fr)_minmax(0,1.6fr)_minmax(76px,1fr)_minmax(96px,1.1fr)_minmax(58px,0.7fr)_minmax(64px,0.8fr)] items-center gap-3 border-b border-line bg-surface-3 px-3.5 text-[10.5px] font-semibold uppercase tracking-[0.1em] text-fg-3 sm:grid">
                                <span/>
                                <span>串口</span>
                                <span>设备</span>
                                <span>运营商</span>
                                <span>本机号码</span>
                                <span>信号</span>
                                <span className="text-right">最后活动</span>
                            </div>

                            {totalCount === 0 ? (
                                <div className="flex flex-col items-center justify-center gap-2 px-6 py-14 text-center">
                                    <span className="flex size-11 items-center justify-center rounded-full border border-line text-fg-3">
                                        <WifiOff className="size-5"/>
                                    </span>
                                    <p className="mt-1 text-sm font-semibold text-fg">未发现已配置的设备</p>
                                    <p className="max-w-sm text-xs leading-5 text-fg-3">
                                        在服务端 config.yaml 的 devices 中配置串口设备后，重启服务即可在此看到每张 SIM 卡的实时状态。
                                    </p>
                                </div>
                            ) : (
                                devices.map((device) => (
                                    <DeviceRow
                                        key={device.id}
                                        device={device}
                                        lastActivity={lastActivityByDevice.get(device.id)}
                                    />
                                ))
                            )}
                        </div>
                    </section>

                    <div className="grid min-h-0 grid-cols-1 gap-3 sm:grid-cols-2">
                        <section className="rounded-md border border-line bg-surface-2 p-3">
                            <div className="mb-2 flex items-baseline gap-2">
                                <h2 className="text-[12px] font-semibold uppercase tracking-[0.14em] text-fg-2">短信构成</h2>
                                <div className="flex-1"/>
                                <span className="num text-[11px] text-fg-3">累计 {flowTotal} 条</span>
                            </div>

                            {/* 真实数据：收/发占比，没有分时数据就不画分时图 */}
                            <div className="flex h-2 overflow-hidden rounded-[1px]">
                                <span className="bg-ok" style={{ width: `${incomingShare}%` }}/>
                                <span className="flex-1 bg-brand/70"/>
                            </div>
                            <div className="mt-2.5 flex items-baseline justify-between">
                                <span className="flex items-baseline gap-1.5">
                                    <span className="size-1.5 rounded-full bg-ok"/>
                                    <span className="text-[11px] text-fg-2">接收</span>
                                    <span className="num text-sm font-semibold text-fg">{incoming}</span>
                                </span>
                                <span className="flex items-baseline gap-1.5">
                                    <span className="text-[11px] text-fg-2">发出</span>
                                    <span className="num text-sm font-semibold text-fg">{outgoing}</span>
                                    <span className="size-1.5 rounded-full bg-brand/70"/>
                                </span>
                            </div>
                            <div className="mt-3 flex items-baseline justify-between border-t border-line-soft pt-2.5">
                                <span className="text-[11px] text-fg-3">今日收发</span>
                                <span className="num text-lg font-semibold text-brand">{stats?.todayCount ?? 0}</span>
                            </div>
                        </section>

                        <section className="rounded-md border border-line bg-surface-2 p-3">
                            <div className="mb-1.5 flex items-baseline gap-2">
                                <h2 className="text-[12px] font-semibold uppercase tracking-[0.14em] text-fg-2">服务状态</h2>
                                <div className="flex-1"/>
                                <span className="num text-[11px] text-fg-3">实时</span>
                            </div>
                            <div className="divide-y divide-line-soft">
                                {serviceRows.map(([label, text, healthy]) => (
                                    <div key={label} className="flex items-center justify-between py-2 text-xs">
                                        <span className="text-fg-2">{label}</span>
                                        <span className={cn('flex items-center gap-1.5 font-semibold', healthy ? 'text-ok' : 'text-fg-3')}>
                                            <span className={cn('size-1.5 rounded-full', healthy ? 'bg-ok' : 'bg-fg-3')}/>
                                            {text}
                                        </span>
                                    </div>
                                ))}
                            </div>
                        </section>
                    </div>
                </div>

                {/* ── 右：未读 + 快捷发送 ── */}
                <div className="flex min-h-0 flex-col gap-3">
                    <section className="flex min-h-0 flex-col">
                        <div className="mb-2 flex items-baseline gap-2.5">
                            <h2 className="text-[12px] font-semibold uppercase tracking-[0.14em] text-fg-2">未读短信</h2>
                            <span className="num text-[11px] text-fg-3">跨设备</span>
                            <div className="flex-1"/>
                            <Link to="/messages" className="text-[11px] font-semibold text-fg-2 transition-colors hover:text-brand">
                                全部 ›
                            </Link>
                        </div>

                        <div className="overflow-hidden rounded-md border border-line bg-surface">
                            <div className="flex h-10 items-center gap-2 border-b border-line px-3.5">
                                <span className="text-[12px] text-fg-2">新消息</span>
                                <span className="num rounded-[3px] bg-brand px-1.5 py-0.5 text-[10.5px] font-bold text-brand-fg">
                                    {unreadList.reduce((sum, item) => sum + item.unreadCount, 0)}
                                </span>
                                <div className="flex-1"/>
                                <Link to="/messages" className="text-[11px] text-fg-2 transition-colors hover:text-brand">
                                    标记已读
                                </Link>
                            </div>

                            {(unreadList.length > 0 ? unreadList : recentList).map((conversation) => {
                                const unread = conversation.unreadCount > 0;
                                return (
                                    <Link
                                        key={`${conversation.deviceId}-${conversation.peer}`}
                                        to="/messages"
                                        className={cn(
                                            'relative flex gap-2.5 border-b border-line-soft px-3 py-2.5 pl-3 last:border-b-0 transition-colors hover:bg-surface-2',
                                        )}
                                    >
                                        {unread && <span className="absolute left-0 top-0 bottom-0 w-0.5 bg-brand" aria-hidden/>}
                                        <span className="num mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-[4px] bg-[var(--av)] text-[10px] font-bold text-fg-2">
                                            {conversation.peer.replace(/\D/g, '').slice(-4, -2) || '??'}
                                        </span>
                                        <div className="min-w-0 flex-1">
                                            <div className="flex items-baseline gap-2">
                                                <span className={cn('num cjk-nowrap text-[12px]', unread ? 'font-semibold text-fg' : 'text-fg-2')}>
                                                    {conversation.peer}
                                                </span>
                                                <span className="num ml-auto text-[10.5px] text-fg-3">
                                                    {formatShortDate(conversation.lastMessage?.timestamp)}
                                                </span>
                                            </div>
                                            <p className={cn('mt-0.5 truncate text-[12.5px]', unread ? 'text-fg-2' : 'text-fg-3')}>
                                                {conversation.lastMessage?.type === 'outgoing' ? '发出：' : ''}
                                                {conversation.lastMessage?.content || '（空消息）'}
                                            </p>
                                            <p className="num mt-1 truncate text-[10px] tracking-[0.03em] text-fg-3">
                                                {conversation.deviceId || '未知设备'}
                                            </p>
                                        </div>
                                    </Link>
                                );
                            })}

                            {conversations.length === 0 && (
                                <div className="px-3 py-10 text-center text-xs text-fg-3">还没有短信记录</div>
                            )}
                        </div>
                    </section>

                    <section className="rounded-md border border-line bg-surface p-3">
                        <div className="mb-2 flex items-baseline gap-2">
                            <h2 className="text-[12px] font-semibold uppercase tracking-[0.14em] text-fg-2">快捷发送</h2>
                            <div className="flex-1"/>
                            <span className="text-[10.5px] text-fg-3">必须选卡</span>
                        </div>

                        <div className="grid grid-cols-[minmax(0,1fr)_96px] gap-2">
                            <label className="flex h-8 items-center gap-1.5 rounded-[4px] border border-line bg-surface-3 px-2">
                                <span className="text-[10.5px] text-fg-3">发送自</span>
                                <select
                                    value={effectiveDeviceId}
                                    onChange={(event) => setComposeDeviceId(event.target.value)}
                                    className="num min-w-0 flex-1 appearance-none bg-transparent text-[12px] text-fg outline-none"
                                    aria-label="发送设备"
                                >
                                    {devices.map((device) => (
                                        <option key={device.id} value={device.id} className="bg-surface text-fg">
                                            {device.name}
                                        </option>
                                    ))}
                                </select>
                            </label>
                            <input
                                value={composeTo}
                                onChange={(event) => setComposeTo(event.target.value)}
                                placeholder="收件人"
                                inputMode="tel"
                                className="num h-8 min-w-0 rounded-[4px] border border-line bg-surface-3 px-2 text-[12px] text-fg outline-none placeholder:text-fg-3 focus:border-brand/60"
                            />
                        </div>

                        <textarea
                            value={composeText}
                            onChange={(event) => setComposeText(event.target.value)}
                            placeholder="短信内容"
                            rows={3}
                            className="mt-2 w-full resize-none rounded-[4px] border border-line bg-surface-3 px-2 py-2 text-[12.5px] leading-relaxed text-fg outline-none placeholder:text-fg-3 focus:border-brand/60"
                        />

                        <div className="mt-2 flex items-center gap-2">
                            <span className="num text-[10.5px] text-fg-3">
                                {composeText.length} / 70
                            </span>
                            <span className="truncate text-[10.5px] text-fg-3">
                                {effectiveDevice
                                    ? `· ${effectiveDevice.name}${effectiveDevice.connected ? '' : '（离线）'}`
                                    : '· 无可用设备'}
                            </span>
                            <div className="flex-1"/>
                            <button
                                type="button"
                                onClick={handleSend}
                                disabled={!canSend || sendMutation.isPending}
                                className="inline-flex h-8 items-center gap-1.5 rounded-[4px] bg-brand px-3 text-xs font-bold text-brand-fg transition-opacity disabled:cursor-not-allowed disabled:opacity-40"
                            >
                                <Send className="size-3.5"/>
                                {sendMutation.isPending ? '发送中' : '发送'}
                            </button>
                        </div>
                    </section>

                    <Link
                        to="/serial"
                        className="flex items-center gap-2 rounded-md border border-line bg-surface-2 px-3 py-2.5 text-xs font-semibold text-fg-2 transition-colors hover:border-brand/40 hover:text-brand"
                    >
                        <Smartphone className="size-4"/>
                        设备与网络控制
                        <ArrowRight className="ml-auto size-3.5"/>
                    </Link>
                </div>
            </div>
        </div>
    );
}