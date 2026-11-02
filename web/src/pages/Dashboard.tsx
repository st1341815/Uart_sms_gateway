import {useEffect, useState} from 'react';
import {Link} from 'react-router-dom';
import {
    ArrowRight,
    Clock3,
    MessageSquareText,
    Radio,
    RefreshCw,
    Send,
    ServerCog,
    Signal,
    Smartphone,
    WifiOff,
} from 'lucide-react';
import {useQuery} from '@tanstack/react-query';
import {getStats} from '../api/messages';
import type {DeviceStatusSummary, Stats} from '../api/types';
import {StatCard} from '@/components/StatsCard.tsx';
import {getAllDevicesStatus} from '@/api/serial.ts';
import {cn} from '@/lib/utils.ts';
import {PageHeader} from '@/components/PageHeader';

/** 信号等级 0-31 → 百分比 */
const signalPercent = (signal?: number) => {
    if (!signal || signal <= 0) return 0;
    return Math.max(0, Math.min(100, Math.round((signal / 31) * 100)));
};

const describeSignal = (signal?: number) => {
    if (!signal) return '无数据';
    if (signal >= 20) return '优秀';
    if (signal >= 15) return '良好';
    if (signal >= 10) return '一般';
    return '较弱';
};

const signalTone = (signal?: number) => {
    const percent = signalPercent(signal);
    if (!signal) return 'bg-slate-300';
    if (percent >= 65) return 'bg-emerald-500';
    if (percent >= 40) return 'bg-amber-500';
    return 'bg-rose-500';
};

export default function Dashboard() {
    const [stats, setStats] = useState<Stats | null>(null);
    const [statsLoading, setStatsLoading] = useState(true);

    useEffect(() => {
        const loadStats = async () => {
            try {
                setStats(await getStats());
            } catch (error) {
                console.error('获取统计信息失败:', error);
            } finally {
                setStatsLoading(false);
            }
        };

        loadStats();
        const interval = window.setInterval(loadStats, 30000);
        return () => window.clearInterval(interval);
    }, []);

    const {
        data: devicesResponse,
        dataUpdatedAt,
        isFetching,
    } = useQuery({
        queryKey: ['allDevicesStatus'],
        queryFn: getAllDevicesStatus,
        refetchInterval: 10000,
    });

    const devices: DeviceStatusSummary[] = devicesResponse?.devices ?? [];
    const onlineDevices = devices.filter((device) => device.connected);
    const onlineCount = onlineDevices.length;
    const totalCount = devices.length;

    // 信号概览取第一台在线设备的信号，用于顶部 KPI
    const leadDevice = onlineDevices[0];
    const ledSignalPercent = leadDevice ? signalPercent(leadDevice.signal) : 0;

    const updatedAt = dataUpdatedAt
        ? new Date(dataUpdatedAt).toLocaleTimeString('zh-CN', {hour: '2-digit', minute: '2-digit'})
        : '—';

    if (statsLoading) {
        return (
            <div className="grid min-h-[55vh] place-items-center">
                <div className="flex items-center gap-3 rounded-xl border border-blue-100 bg-blue-50 px-5 py-3 text-sm font-medium text-blue-700">
                    <RefreshCw className="size-4 animate-spin"/>
                    正在同步设备数据
                </div>
            </div>
        );
    }

    return (
        <div>
            <PageHeader
                title="设备概览"
                description="多设备短信转发运行时状态：串口链路、蜂窝网络与短信流量"
                action={<div className="flex flex-wrap items-center gap-2">
                    <span className="mr-1 hidden text-xs text-slate-400 md:inline">
                        {isFetching ? '正在同步' : `更新于 ${updatedAt}`}
                    </span>
                    <Link
                        to="/messages"
                        className="inline-flex h-9 items-center gap-2 rounded-lg bg-blue-600 px-3.5 text-xs font-semibold text-white transition-colors hover:bg-blue-700"
                    >
                        <Send className="size-4"/>发送短信
                    </Link>
                </div>}
            />

            <section className="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
                <StatCard
                    label="在线设备"
                    value={onlineCount}
                    unit={`/ ${totalCount}`}
                    icon={ServerCog}
                    colorClass={onlineCount === totalCount && totalCount > 0 ? 'bg-blue-50 text-blue-700' : 'bg-amber-50 text-amber-600'}
                    subValue={totalCount === 0 ? '尚未配置设备' : onlineCount === totalCount ? '全部链路正常' : `${totalCount - onlineCount} 台离线`}
                />
                <StatCard
                    label="主设备信号"
                    value={leadDevice ? ledSignalPercent : '—'}
                    unit={leadDevice ? '%' : undefined}
                    icon={Signal}
                    colorClass="bg-blue-50 text-blue-700"
                    subValue={leadDevice
                        ? `${leadDevice.name} · ${describeSignal(leadDevice.signal)} · ${leadDevice.signal ?? '—'}`
                        : '等待设备连接'}
                />
                <StatCard
                    label="短信总量"
                    value={stats?.totalCount || 0}
                    icon={MessageSquareText}
                    colorClass="bg-blue-50 text-blue-700"
                    subValue={`接收 ${stats?.incomingCount || 0} · 发出 ${stats?.outgoingCount || 0}`}
                />
                <StatCard
                    label="今日短信"
                    value={stats?.todayCount || 0}
                    icon={Clock3}
                    colorClass="bg-blue-600 text-white"
                    subValue="今日累计收发"
                />
            </section>

            <section className="mt-4 grid items-start gap-4 lg:grid-cols-[minmax(0,1.55fr)_minmax(280px,0.65fr)]">
                <article className="overflow-hidden rounded-2xl border border-slate-200 bg-white">
                    <div className="flex items-center justify-between border-b border-slate-100 px-5 py-3.5">
                        <div className="flex items-center gap-3">
                            <span className="flex size-9 items-center justify-center rounded-xl bg-blue-50 text-blue-700">
                                <Smartphone className="size-[18px]"/>
                            </span>
                            <div>
                                <h2 className="text-sm font-bold text-slate-900">设备矩阵</h2>
                                <p className="text-xs text-slate-400">
                                    {totalCount > 0 ? `${onlineCount} / ${totalCount} 台串口链路在线` : '等待串口设备接入'}
                                </p>
                            </div>
                        </div>
                        <Link
                            to="/serial"
                            className="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-[11px] font-semibold text-blue-700 transition hover:bg-blue-50"
                        >
                            串口控制
                            <ArrowRight className="size-3"/>
                        </Link>
                    </div>

                    {totalCount === 0 ? (
                        <div className="flex flex-col items-center justify-center gap-2 px-6 py-14 text-center">
                            <span className="flex size-12 items-center justify-center rounded-full bg-slate-100 text-slate-400">
                                <WifiOff className="size-6"/>
                            </span>
                            <p className="mt-2 text-sm font-semibold text-slate-700">未发现已配置的设备</p>
                            <p className="max-w-sm text-xs leading-5 text-slate-400">
                                在服务端 config.yaml 的 devices 中配置串口设备后，重启服务即可在此看到每张 SIM 卡的实时状态。
                            </p>
                        </div>
                    ) : (
                        <div className="grid gap-3 p-4 sm:grid-cols-2">
                            {devices.map((device) => {
                                const percent = signalPercent(device.signal);
                                return (
                                    <div
                                        key={device.id}
                                        className={cn(
                                            'rounded-xl border p-4 transition-colors',
                                            device.connected
                                                ? 'border-slate-200 bg-white hover:border-blue-300'
                                                : 'border-slate-200 bg-slate-50/70',
                                        )}
                                    >
                                        <div className="flex items-start justify-between gap-3">
                                            <div className="flex min-w-0 items-center gap-2">
                                                <span className={cn(
                                                    'relative flex size-2.5 shrink-0 rounded-full',
                                                    device.connected ? 'bg-emerald-500' : 'bg-slate-300',
                                                )}>
                                                    {device.connected && <span className="absolute inset-0 animate-ping rounded-full bg-emerald-500 opacity-50"/>}
                                                </span>
                                                <div className="min-w-0">
                                                    <p className="truncate text-sm font-bold text-slate-900">{device.name}</p>
                                                    <p className="mt-0.5 truncate font-mono text-[10px] text-slate-400">
                                                        {device.port || device.id}
                                                    </p>
                                                </div>
                                            </div>
                                            <span className={cn(
                                                'shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold',
                                                device.flymode
                                                    ? 'bg-amber-50 text-amber-700'
                                                    : device.connected ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500',
                                            )}>
                                                {device.connected ? (device.flymode ? '飞行模式' : '在线') : '离线'}
                                            </span>
                                        </div>

                                        <div className="mt-3.5 flex items-baseline gap-1.5">
                                            <span className="text-xl font-bold tracking-[-0.04em] text-slate-900">
                                                {device.connected ? (device.operator || '未知运营商') : '—'}
                                            </span>
                                            {device.isRoaming && device.connected && (
                                                <span className="rounded bg-amber-50 px-1.5 py-0.5 text-[9px] font-semibold text-amber-700">漫游</span>
                                            )}
                                        </div>

                                        <div className="mt-3">
                                            <div className="flex items-center justify-between text-[10px] font-medium text-slate-400">
                                                <span>信号强度</span>
                                                <span className="font-mono">{device.connected ? `${percent}%` : '—'}</span>
                                            </div>
                                            <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-slate-100">
                                                <div
                                                    className={cn('h-full rounded-full transition-all', device.connected ? signalTone(device.signal) : 'bg-slate-200')}
                                                    style={{width: `${device.connected ? percent : 0}%`}}
                                                />
                                            </div>
                                        </div>

                                        <dl className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-slate-100 pt-2.5 text-[10px] text-slate-400">
                                            <div className="flex items-center gap-1">
                                                <span>号码</span>
                                                <span className="font-mono text-slate-600">{device.phoneNumber || '—'}</span>
                                            </div>
                                            <div className="flex items-center gap-1">
                                                <span>CSQ</span>
                                                <span className="font-mono text-slate-600">{device.connected ? (device.signal ?? '—') : '—'}</span>
                                            </div>
                                        </dl>
                                    </div>
                                );
                            })}
                        </div>
                    )}
                </article>

                <div className="grid gap-4">
                    <article className="rounded-2xl border border-slate-200 bg-white p-5">
                        <div className="flex items-center justify-between">
                            <div>
                                <h2 className="text-sm font-bold text-slate-900">服务状态</h2>
                                <p className="mt-1 text-xs text-slate-400">通信服务实时检查</p>
                            </div>
                            <span className={cn(
                                'size-2 rounded-full',
                                onlineCount > 0 ? 'bg-emerald-500' : 'bg-slate-300',
                            )}/>
                        </div>
                        <div className="mt-4 divide-y divide-slate-100">
                            {[
                                ['串口链路', onlineCount > 0 ? `${onlineCount} 台在线` : '等待', onlineCount > 0],
                                ['短信监听服务', onlineCount > 0 ? '正常' : '等待', onlineCount > 0],
                                ['短信入库', '正常', true],
                            ].map(([label, text, healthy]) => (
                                <div key={String(label)} className="flex items-center justify-between py-3 text-xs first:pt-0 last:pb-0">
                                    <span className="text-slate-500">{label}</span>
                                    <span className={cn('flex items-center gap-1.5 font-semibold', healthy ? 'text-emerald-600' : 'text-slate-400')}>
                                        <span className={cn('size-1.5 rounded-full', healthy ? 'bg-emerald-500' : 'bg-slate-300')}/>
                                        {String(text)}
                                    </span>
                                </div>
                            ))}
                        </div>
                    </article>

                    <article className="rounded-2xl border border-slate-200 bg-white p-5">
                        <div className="flex items-center gap-2">
                            <Radio className="size-4 text-blue-600"/>
                            <h2 className="text-sm font-bold text-slate-900">快捷操作</h2>
                        </div>
                        <div className="mt-4 grid gap-2">
                            <Link
                                to="/messages"
                                className="flex items-center justify-between rounded-xl border border-slate-200 px-3.5 py-2.5 text-xs font-semibold text-slate-700 transition hover:border-blue-200 hover:bg-blue-50/60 hover:text-blue-700"
                            >
                                <span className="flex items-center gap-2"><MessageSquareText className="size-4"/>查看短信会话</span>
                                <ArrowRight className="size-3.5"/>
                            </Link>
                            <Link
                                to="/serial"
                                className="flex items-center justify-between rounded-xl border border-slate-200 px-3.5 py-2.5 text-xs font-semibold text-slate-700 transition hover:border-blue-200 hover:bg-blue-50/60 hover:text-blue-700"
                            >
                                <span className="flex items-center gap-2"><Smartphone className="size-4"/>设备与网络控制</span>
                                <ArrowRight className="size-3.5"/>
                            </Link>
                            <Link
                                to="/notifications"
                                className="flex items-center justify-between rounded-xl border border-slate-200 px-3.5 py-2.5 text-xs font-semibold text-slate-700 transition hover:border-blue-200 hover:bg-blue-50/60 hover:text-blue-700"
                            >
                                <span className="flex items-center gap-2"><Radio className="size-4"/>配置通知渠道</span>
                                <ArrowRight className="size-3.5"/>
                            </Link>
                        </div>
                    </article>
                </div>
            </section>
        </div>
    );
}
