import {useState} from 'react';
import type {ReactNode} from 'react';
import {Activity, Loader2, MessageSquareText, Pencil, RadioTower, RotateCcw, Send, Signal, Smartphone} from 'lucide-react';
import {Link} from 'react-router-dom';
import {toast} from 'sonner';
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query';
import * as serialApi from '../api/serial';
import {Button} from '@/components/ui/button';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import {Input} from '@/components/ui/input';
import {Textarea} from '@/components/ui/textarea';
import {PageHeader} from '@/components/PageHeader';
import {cn} from '@/lib/utils';

const UNAVAILABLE = '—';

interface InfoRowProps {
    label: string;
    value: ReactNode;
    mono?: boolean;
}

function InfoRow({label, value, mono = false}: InfoRowProps) {
    return (
        <div className="flex min-w-0 items-center justify-between gap-4 border-b border-slate-100 py-2.5 last:border-b-0">
            <dt className="shrink-0 text-xs font-medium text-slate-500">{label}</dt>
            <dd className={cn('min-w-0 truncate text-right text-xs font-semibold text-slate-800', mono && 'font-mono')}>
                {value}
            </dd>
        </div>
    );
}

interface StatusTileProps {
    label: string;
    value: string;
    tone?: 'blue' | 'green' | 'amber' | 'slate';
}

function StatusTile({label, value, tone = 'slate'}: StatusTileProps) {
    const toneClass = {
        blue: 'border-blue-100 bg-blue-50 text-blue-700',
        green: 'border-emerald-100 bg-emerald-50 text-emerald-700',
        amber: 'border-amber-200 bg-amber-50 text-amber-700',
        slate: 'border-slate-200 bg-slate-50 text-slate-600',
    }[tone];

    return (
        <div className={cn('rounded-xl border px-3.5 py-3', toneClass)}>
            <p className="text-[10px] font-semibold tracking-wide opacity-70">{label}</p>
            <p className="mt-1 truncate text-sm font-bold">{value}</p>
        </div>
    );
}

/** 信号等级 0-31 → 百分比 */
const signalPercent = (signal?: number) => {
    if (!signal || signal <= 0) return 0;
    return Math.max(0, Math.min(100, Math.round((signal / 31) * 100)));
};

export default function SerialControl() {
    // 用户手动选择的设备 id；空 = 用默认回落值（见下面的派生逻辑）
    const [deviceSelection, setDeviceSelection] = useState('');
    const [to, setTo] = useState('');
    const [content, setContent] = useState('');

    // 改名弹窗：renameTarget 为 null 表示关闭
    const [renameTarget, setRenameTarget] = useState<{id: string; name: string} | null>(null);
    const [renameValue, setRenameValue] = useState('');

    // 本机号码补录弹窗：境外/漫游卡不写 SIM 卡的 MSISDN 字段，AT+CNUM 必然读不到，
    // 只能手工补录。phoneTarget 为 null 表示关闭。
    const [phoneTarget, setPhoneTarget] = useState<{id: string; number: string} | null>(null);
    const [phoneValue, setPhoneValue] = useState('');
    const queryClient = useQueryClient();

    const {
        data: devicesResponse,
        isFetching: devicesFetching,
        isLoading: devicesLoading,
        refetch: refetchDevices,
    } = useQuery({
        queryKey: ['allDevicesStatus'],
        queryFn: serialApi.getAllDevicesStatus,
        refetchInterval: 10000,
    });

    const devices = devicesResponse?.devices ?? [];

    // 默认选中第一台在线设备（没有在线设备则选第一台）。
    //
    // 这里刻意用「派生值」而不是 useEffect + setState：
    //   - 用户手动选过（deviceSelection 非空且仍在列表里）→ 用用户的选择
    //   - 否则回落到第一台在线设备 / 第一台设备
    // 旧写法在 effect 里同步 setState，react-hooks/set-state-in-effect 会报
    // 「级联渲染」错误，而且 devices 每次轮询都会换新数组引用，容易反复触发。
    const preferredId = devices.some((device) => device.id === deviceSelection)
        ? deviceSelection
        : '';
    const selectedDeviceId = preferredId ||
        (devices.find((device) => device.connected) ?? devices[0])?.id ||
        '';
    const setSelectedDeviceId = setDeviceSelection;

    const selectedDevice = devices.find((device) => device.id === selectedDeviceId);
    const connected = Boolean(selectedDevice?.connected);
    const unavailable = UNAVAILABLE;

    const sendMutation = useMutation({
        mutationFn: (payload: {to: string; content: string; device_id: string}) => serialApi.sendSMS(payload),
        onSuccess: () => {
            toast.success(`短信下发成功（${selectedDevice?.name ?? '默认设备'}），等待设备确认`);
            setTo('');
            setContent('');
        },
        onError: (error: unknown) => {
            console.error('发送失败:', error);
            toast.error(error instanceof Error ? error.message : '发送失败');
        },
    });

    const setFlymodeMutation = useMutation({
        mutationFn: (enabled: boolean) => serialApi.setFlymode(enabled, selectedDeviceId),
        onSuccess: async () => {
            toast.success(`设置成功（${selectedDevice?.name ?? '默认设备'}）`);
            await refetchDevices();
        },
        onError: () => toast.error('操作失败'),
    });

    // 改名：成功后让设备列表立刻刷新，同时让消息页的设备下拉也跟着更新
    const renameMutation = useMutation({
        mutationFn: ({id, name}: {id: string; name: string}) => serialApi.renameDevice(id, name),
        onSuccess: async (data) => {
            toast.success(`名称已改为「${data.name}」`);
            setRenameTarget(null);
            await queryClient.invalidateQueries({queryKey: ['allDevicesStatus']});
            await queryClient.invalidateQueries({queryKey: ['devices']});
        },
        onError: (error: unknown) => {
            console.error('改名失败:', error);
            toast.error(error instanceof Error ? error.message : '改名失败');
        },
    });

    // 打开改名弹窗：预填当前名称
    const openRenameDialog = (device: {id: string; name: string}) => {
        setRenameTarget(device);
        setRenameValue(device.name);
    };

    const handleRenameSubmit = (event: React.FormEvent) => {
        event.preventDefault();
        if (!renameTarget) return;
        const next = renameValue.trim();
        if (!next) {
            toast.warning('名称不能为空');
            return;
        }
        if (next === renameTarget.name) {
            setRenameTarget(null);
            return;
        }
        renameMutation.mutate({id: renameTarget.id, name: next});
    };

    // 本机号码补录：国内卡一般能直接读到；境外/漫游卡多数返回空，只能手工填。
    const phoneMutation = useMutation({
        mutationFn: ({id, number}: {id: string; number: string}) => serialApi.setDevicePhoneNumber(id, number),
        onSuccess: async (data) => {
            toast.success(data.phoneNumber ? `本机号码已设为 ${data.phoneNumber}` : '已清除补录号码，回退到 SIM 卡读取值');
            setPhoneTarget(null);
            await queryClient.invalidateQueries({queryKey: ['allDevicesStatus']});
            await queryClient.invalidateQueries({queryKey: ['devices']});
        },
        onError: (error: unknown) => {
            console.error('设置本机号码失败:', error);
            toast.error(error instanceof Error ? error.message : '设置本机号码失败');
        },
    });

    const openPhoneDialog = (device: {id: string; phoneNumber?: string}) => {
        setPhoneTarget({id: device.id, number: device.phoneNumber || ''});
        setPhoneValue(device.phoneNumber || '');
    };

    const handlePhoneSubmit = (event: React.FormEvent) => {
        event.preventDefault();
        if (!phoneTarget) return;
        const next = phoneValue.trim();
        // 允许留空提交 = 清除补录
        if (next === phoneTarget.number) {
            setPhoneTarget(null);
            return;
        }
        phoneMutation.mutate({id: phoneTarget.id, number: next});
    };

    const rebootMutation = useMutation({
        mutationFn: () => serialApi.rebootMcu(selectedDeviceId),
        onSuccess: async () => {
            toast.success(`模块重启命令已发送（${selectedDevice?.name ?? '默认设备'}）`);
            await refetchDevices();
        },
        onError: () => toast.error('操作失败'),
    });

    const handleSend = (event: React.FormEvent) => {
        event.preventDefault();
        if (!to.trim() || !content.trim()) {
            toast.warning('请输入手机号和短信内容');
            return;
        }
        if (!selectedDeviceId) {
            toast.warning('请选择发送设备');
            return;
        }
        sendMutation.mutate({to: to.trim(), content: content.trim(), device_id: selectedDeviceId});
    };

    if (devicesLoading) {
        return (
            <div className="flex min-h-[50vh] items-center justify-center gap-2 text-sm text-slate-500">
                <Loader2 className="size-4 animate-spin text-blue-600"/>
                正在读取设备列表
            </div>
        );
    }

    return (
        <div>
            <PageHeader
                title="串口控制"
                description="选择一张 SIM 卡，查看移动网络参数、发送短信或执行设备控制。"
                action={<span className="text-xs text-slate-400">
                    {devicesFetching ? '正在同步' : `${devices.filter((d) => d.connected).length}/${devices.length} 台在线`}
                </span>}
            />

            {/* 设备切换 */}
            <Card className="mt-6 gap-0 py-0">
                <CardHeader className="border-b border-slate-100 py-4">
                    <CardTitle className="flex items-center gap-2 text-sm">
                        <RadioTower className="size-4 text-blue-600"/>
                        设备选择
                        <span className="text-[11px] font-normal text-slate-400">共 {devices.length} 台</span>
                    </CardTitle>
                </CardHeader>
                <CardContent className="p-3">
                    {devices.length === 0 ? (
                        <p className="px-2 py-6 text-center text-xs leading-6 text-slate-400">
                            未发现已配置的串口设备，请在服务端 config.yaml 的 devices 中配置后重启服务。
                        </p>
                    ) : (
                        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
                            {devices.map((device) => {
                                const active = device.id === selectedDeviceId;
                                const percent = signalPercent(device.signal);
                                return (
                                    <div
                                        key={device.id}
                                        className={cn(
                                            'group relative flex min-w-0 items-center gap-3 rounded-xl border p-3 transition-colors',
                                            active
                                                ? 'border-blue-200 bg-blue-50/70'
                                                : 'border-slate-200 hover:border-blue-200 hover:bg-slate-50',
                                        )}
                                    >
                                        <button
                                            type="button"
                                            onClick={() => setSelectedDeviceId(device.id)}
                                            aria-pressed={active}
                                            className="flex min-w-0 flex-1 items-center gap-3 text-left outline-none focus-visible:rounded-lg focus-visible:ring-2 focus-visible:ring-blue-300"
                                        >
                                            <span className={cn(
                                                'flex size-9 shrink-0 items-center justify-center rounded-lg',
                                                active ? 'bg-white text-blue-700' : 'bg-slate-100 text-slate-500',
                                                !device.connected && 'text-slate-400',
                                            )}>
                                                <Smartphone className="size-4"/>
                                            </span>
                                            <span className="min-w-0 flex-1">
                                                <span className="flex items-center gap-1.5">
                                                    <span className="truncate text-sm font-semibold text-slate-900">{device.name}</span>
                                                    <span className={cn('size-1.5 shrink-0 rounded-full', device.connected ? 'bg-emerald-500' : 'bg-slate-300')}/>
                                                </span>
                                                <span className="mt-0.5 flex items-center gap-2 text-[10px] text-slate-400">
                                                    <span className="truncate font-mono">{device.port || device.id}</span>
                                                    <span className="shrink-0">{device.connected ? `${percent}%` : '离线'}</span>
                                                </span>
                                            </span>
                                        </button>
                                        {/* 改名入口：桌面端 hover / 键盘聚焦时显现，触屏（<sm）常显 */}
                                        <button
                                            type="button"
                                            title="修改卡名称"
                                            aria-label={`修改 ${device.name} 的名称`}
                                            onClick={(event) => {
                                                event.stopPropagation();
                                                openRenameDialog(device);
                                            }}
                                            className={cn(
                                                'flex size-7 shrink-0 items-center justify-center rounded-md text-slate-400 transition',
                                                'hover:bg-white hover:text-blue-600',
                                                'sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100',
                                            )}
                                        >
                                            <Pencil className="size-3.5"/>
                                        </button>
                                    </div>
                                );
                            })}
                        </div>
                    )}
                </CardContent>
            </Card>

            <div className="mt-4 grid items-start gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(340px,0.65fr)]">
                <div className="grid gap-4">
                    {/* 网络与设备状态 */}
                    <Card className="gap-0 overflow-hidden py-0">
                        <CardHeader className="border-b border-slate-100 py-5">
                            <div className="flex items-center justify-between gap-4">
                                <div>
                                    <CardTitle className="flex items-center gap-2 text-base">
                                        <Signal className="size-4 text-blue-600"/>
                                        网络与设备状态
                                        {selectedDevice && (
                                            <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-semibold text-slate-500">
                                                {selectedDevice.name}
                                            </span>
                                        )}
                                    </CardTitle>
                                    <p className="mt-1.5 text-xs text-slate-500">蜂窝网络、信号参数与串口模块信息</p>
                                </div>
                                <span className={cn(
                                    'inline-flex shrink-0 items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-semibold',
                                    connected
                                        ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                                        : 'border-rose-200 bg-rose-50 text-rose-700',
                                )}>
                                    <span className={cn('size-1.5 rounded-full', connected ? 'bg-emerald-500' : 'bg-rose-500')}/>
                                    {connected ? '设备在线' : '设备离线'}
                                </span>
                            </div>
                        </CardHeader>

                        <CardContent className="py-5">
                            {!selectedDevice ? (
                                <div className="flex min-h-60 flex-col items-center justify-center gap-2 text-center text-slate-400">
                                    <Smartphone className="size-10 opacity-25"/>
                                    <p className="text-sm font-medium">请选择一台设备</p>
                                    <p className="text-xs">选择后可查看该 SIM 卡的实时网络状态</p>
                                </div>
                            ) : (
                                <div>
                                    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                                        <StatusTile
                                            label="运营商"
                                            value={connected ? selectedDevice.operator || unavailable : unavailable}
                                            tone={connected ? 'blue' : 'slate'}
                                        />
                                        {/* 本机号码：可手工补录。
                                            境外/漫游卡不写 SIM 卡 MSISDN 字段，AT+CNUM 必然读不到，
                                            只写「读取不到」会让用户以为是坏了，所以直接给补录入口。 */}
                                        <div className={cn(
                                            'group/phone relative rounded-xl border px-3.5 py-3',
                                            connected ? 'border-blue-100 bg-blue-50 text-blue-700' : 'border-slate-200 bg-slate-50 text-slate-600',
                                        )}>
                                            <p className="text-[10px] font-semibold tracking-wide opacity-70">本机号码</p>
                                            <p className="mt-1 truncate text-sm font-bold">
                                                {connected ? selectedDevice.phoneNumber || unavailable : unavailable}
                                            </p>
                                            <button
                                                type="button"
                                                title="手工补录本机号码"
                                                aria-label="手工补录本机号码"
                                                onClick={() => openPhoneDialog(selectedDevice)}
                                                className="absolute right-1.5 top-1.5 rounded p-1 text-current opacity-0 transition hover:bg-white/60 focus-visible:opacity-100 group-hover/phone:opacity-60 group-hover/phone:hover:opacity-100"
                                            >
                                                <Pencil className="size-3"/>
                                            </button>
                                        </div>
                                        <StatusTile
                                            label="信号等级"
                                            value={connected ? `${selectedDevice.signal ?? unavailable}` : unavailable}
                                            tone={connected ? 'green' : 'slate'}
                                        />
                                        <StatusTile
                                            label="飞行模式"
                                            value={connected ? selectedDevice.flymode ? '已开启' : '已关闭' : unavailable}
                                            tone={connected && selectedDevice.flymode ? 'amber' : connected ? 'green' : 'slate'}
                                        />
                                    </div>

                                    <section className="mt-5">
                                        <div className="mb-2.5 flex items-center justify-between">
                                            <h3 className="text-xs font-bold text-slate-800">信号质量</h3>
                                            <span className="text-[10px] font-medium text-slate-400">
                                                {connected ? `CSQ ${selectedDevice.signal ?? '—'} / 31` : '等待设备连接'}
                                            </span>
                                        </div>
                                        <div className="h-2 overflow-hidden rounded-full bg-slate-100">
                                            <div
                                                className={cn(
                                                    'h-full rounded-full transition-all',
                                                    percentToTone(signalPercent(selectedDevice.signal)),
                                                )}
                                                style={{width: `${connected ? signalPercent(selectedDevice.signal) : 0}%`}}
                                            />
                                        </div>
                                    </section>

                                    <section className="mt-5 border-t border-slate-100 pt-4">
                                        <h3 className="mb-1 text-xs font-bold text-slate-800">模块信息</h3>
                                        <dl className="grid gap-x-8 sm:grid-cols-2">
                                            <InfoRow label="设备标识" value={selectedDevice.id} mono/>
                                            <InfoRow label="串口设备" value={selectedDevice.port || unavailable} mono/>
                                            <InfoRow label="运营商" value={connected ? selectedDevice.operator || unavailable : unavailable}/>
                                            <InfoRow
                                                label="漫游状态"
                                                value={connected
                                                    ? selectedDevice.isRoaming
                                                        ? <span className="text-amber-600">漫游中</span>
                                                        : <span className="text-emerald-600">本地</span>
                                                    : unavailable}
                                            />
                                        </dl>

                                        <div className="mt-3 grid gap-2 sm:grid-cols-2">
                                            {[
                                                ['ICCID', connected ? selectedDevice.iccid || unavailable : unavailable],
                                                ['IMSI', connected ? selectedDevice.imsi || unavailable : unavailable],
                                            ].map(([label, value]) => (
                                                <div key={label} className="min-w-0 rounded-lg bg-slate-50 px-3.5 py-3">
                                                    <p className="text-[10px] font-semibold text-slate-400">{label}</p>
                                                    <p className="mt-1 truncate font-mono text-xs font-semibold text-slate-700" title={String(value)}>{value}</p>
                                                </div>
                                            ))}
                                        </div>
                                    </section>
                                </div>
                            )}
                        </CardContent>
                    </Card>

                    {/* 发送短信 */}
                    <Card className="gap-0 py-0">
                        <CardHeader className="border-b border-slate-100 py-5">
                            <CardTitle className="flex items-center gap-2 text-base">
                                <Send className="size-4 text-blue-600"/>
                                发送短信
                                {selectedDevice && (
                                    <span className="text-[11px] font-normal text-slate-400">通过 {selectedDevice.name}</span>
                                )}
                            </CardTitle>
                            <p className="mt-1.5 text-xs text-slate-500">选择设备后填写内容，直接从该 SIM 卡下发</p>
                        </CardHeader>
                        <CardContent className="py-5">
                            <form onSubmit={handleSend} className="grid gap-4">
                                <div className="grid gap-4 sm:grid-cols-[220px_minmax(0,1fr)]">
                                    <div className="space-y-1.5">
                                        <label htmlFor="serial-to" className="block text-xs font-semibold text-slate-700">目标号码</label>
                                        <Input
                                            id="serial-to"
                                            type="tel"
                                            value={to}
                                            onChange={(event) => setTo(event.target.value)}
                                            placeholder="10086"
                                            className="bg-slate-50 font-mono focus:bg-white"
                                        />
                                    </div>
                                    <div className="space-y-1.5">
                                        <div className="flex items-center justify-between">
                                            <label htmlFor="serial-content" className="block text-xs font-semibold text-slate-700">短信内容</label>
                                            <span className="text-[10px] text-slate-400">{content.length} 字</span>
                                        </div>
                                        <Textarea
                                            id="serial-content"
                                            value={content}
                                            onChange={(event) => setContent(event.target.value)}
                                            placeholder="请输入短信内容"
                                            className="min-h-20 resize-none bg-slate-50 focus:bg-white"
                                        />
                                    </div>
                                </div>
                                <div className="flex flex-wrap items-center justify-between gap-3">
                                    <p className="text-xs leading-5 text-slate-400">
                                        部分设备对短信长度有限制，建议单条不超过 70 字。
                                    </p>
                                    <Button
                                        type="submit"
                                        disabled={!connected || sendMutation.isPending}
                                        className="h-10 bg-blue-600 px-5 text-white hover:bg-blue-700"
                                    >
                                        {sendMutation.isPending
                                            ? <Loader2 className="size-4 animate-spin"/>
                                            : <Send className="size-4"/>}
                                        {sendMutation.isPending ? '发送中...' : selectedDevice ? `通过 ${selectedDevice.name} 发送` : '发送短信'}
                                    </Button>
                                </div>
                            </form>
                        </CardContent>
                    </Card>
                </div>

                {/* 设备控制 */}
                <Card className="gap-0 py-0 xl:sticky xl:top-[84px]">
                    <CardHeader className="border-b border-slate-100 py-5">
                        <CardTitle className="flex items-center gap-2 text-base">
                            <Activity className="size-4 text-blue-600"/>
                            设备控制
                        </CardTitle>
                        <p className="mt-1.5 text-xs text-slate-500">切换蜂窝网络状态或重启模块</p>
                    </CardHeader>
                    <CardContent className="py-5">
                        <dl className="mb-4 rounded-xl border border-slate-200 bg-slate-50 px-3.5">
                            <InfoRow label="当前设备" value={selectedDevice?.name ?? unavailable}/>
                            <InfoRow
                                label="串口连接"
                                value={<span className={connected ? 'text-emerald-600' : 'text-rose-600'}>{connected ? '已连接' : '未连接'}</span>}
                            />
                            <InfoRow
                                label="飞行模式"
                                value={connected ? selectedDevice?.flymode ? '已开启' : '已关闭' : unavailable}
                            />
                        </dl>
                        <div className="grid gap-2">
                            <Button
                                onClick={() => setFlymodeMutation.mutate(!selectedDevice?.flymode)}
                                disabled={!connected || setFlymodeMutation.isPending}
                                className="h-10 bg-blue-600 text-white hover:bg-blue-700"
                            >
                                {setFlymodeMutation.isPending ? <Loader2 className="size-4 animate-spin"/> : <Signal className="size-4"/>}
                                {selectedDevice?.flymode ? '关闭飞行模式' : '开启飞行模式'}
                            </Button>
                            <Button
                                onClick={() => rebootMutation.mutate()}
                                disabled={!connected || rebootMutation.isPending}
                                variant="outline"
                                className="h-10 border-rose-200 text-rose-600 hover:bg-rose-50 hover:text-rose-700"
                            >
                                {rebootMutation.isPending ? <Loader2 className="size-4 animate-spin"/> : <RotateCcw className="size-4"/>}
                                重启模块
                            </Button>
                        </div>
                        {!connected && (
                            <p className="mt-3 text-xs leading-5 text-slate-400">
                                {selectedDevice ? '设备连接后才能执行控制操作。' : '请先在左侧选择设备。'}
                            </p>
                        )}

                        <div className="mt-5 rounded-xl border border-amber-200 bg-amber-50/70 px-3.5 py-3 text-xs leading-5 text-amber-900">
                            开启飞行模式期间该设备无法收发短信，通知也会中断。
                        </div>

                        <div className="mt-4 border-t border-slate-100 pt-4">
                            <p className="mb-3 text-xs leading-5 text-slate-500">查看历史消息、会话与设备来源已统一在短信中心。</p>
                            <Button variant="outline" className="h-10 w-full border-blue-200 text-blue-700 hover:bg-blue-50" asChild>
                                <Link to="/messages">
                                    <MessageSquareText className="size-4"/>
                                    前往短信中心
                                </Link>
                            </Button>
                        </div>
                    </CardContent>
                </Card>
            </div>

            {/* 修改卡名称（SIM 卡别名，如「联通3113」） */}
            <Dialog
                open={renameTarget !== null}
                onOpenChange={(open) => {
                    if (!open) setRenameTarget(null);
                }}
            >
                <DialogContent className="sm:max-w-md">
                    <form onSubmit={handleRenameSubmit}>
                        <DialogHeader>
                            <DialogTitle>修改卡名称</DialogTitle>
                            <DialogDescription>
                                改的是这张卡的显示名，只影响界面；不影响设备 ID
                                <span className="font-mono text-slate-500"> {renameTarget?.id}</span>
                                与串口绑定。
                            </DialogDescription>
                        </DialogHeader>

                        <div className="space-y-1.5 py-5">
                            <label htmlFor="rename-device-name" className="block text-sm font-medium text-slate-800">
                                名称
                            </label>
                            <Input
                                id="rename-device-name"
                                value={renameValue}
                                onChange={(event) => setRenameValue(event.target.value)}
                                placeholder="例如：联通3113"
                                maxLength={32}
                                autoFocus
                                className="bg-slate-50 focus:bg-white"
                            />
                            <p className="text-[11px] text-slate-400">
                                最多 32 个字符，{renameValue.trim().length}/32
                            </p>
                        </div>

                        <DialogFooter>
                            <Button
                                type="button"
                                variant="outline"
                                onClick={() => setRenameTarget(null)}
                                disabled={renameMutation.isPending}
                            >
                                取消
                            </Button>
                            <Button
                                type="submit"
                                className="bg-blue-600 text-white hover:bg-blue-700"
                                disabled={renameMutation.isPending || !renameValue.trim()}
                            >
                                {renameMutation.isPending && <Loader2 className="size-4 animate-spin"/>}
                                {renameMutation.isPending ? '保存中...' : '保存'}
                            </Button>
                        </DialogFooter>
                    </form>
                </DialogContent>
            </Dialog>

            {/* 手工补录本机号码（境外/漫游卡不写 SIM 卡 MSISDN，读取必然为空） */}
            <Dialog
                open={phoneTarget !== null}
                onOpenChange={(open) => {
                    if (!open) setPhoneTarget(null);
                }}
            >
                <DialogContent className="sm:max-w-md">
                    <form onSubmit={handlePhoneSubmit}>
                        <DialogHeader>
                            <DialogTitle>本机号码</DialogTitle>
                            <DialogDescription>
                                国内运营商的 SIM 卡一般能自动读到号码；境外卡/漫游卡多数不写这个字段，
                                读取结果会为空，可在这里手工补录。只影响界面显示，不影响实际收发。
                            </DialogDescription>
                        </DialogHeader>

                        <div className="space-y-1.5 py-5">
                            <label htmlFor="device-phone-number" className="block text-sm font-medium text-slate-800">
                                号码
                                <span className="ml-2 font-mono text-[11px] text-slate-400">{phoneTarget?.id}</span>
                            </label>
                            <Input
                                id="device-phone-number"
                                value={phoneValue}
                                onChange={(event) => setPhoneValue(event.target.value)}
                                placeholder="例如：+8613800138000"
                                maxLength={32}
                                autoFocus
                                className="bg-slate-50 focus:bg-white"
                            />
                            <p className="text-[11px] text-slate-400">
                                可含数字、+、-、空格，最多 32 个字符。留空保存＝清除补录，回退到 SIM 卡读取值。
                            </p>
                        </div>

                        <DialogFooter>
                            <Button
                                type="button"
                                variant="outline"
                                onClick={() => setPhoneTarget(null)}
                                disabled={phoneMutation.isPending}
                            >
                                取消
                            </Button>
                            <Button
                                type="submit"
                                className="bg-blue-600 text-white hover:bg-blue-700"
                                disabled={phoneMutation.isPending}
                            >
                                {phoneMutation.isPending && <Loader2 className="size-4 animate-spin"/>}
                                {phoneMutation.isPending ? '保存中...' : '保存'}
                            </Button>
                        </DialogFooter>
                    </form>
                </DialogContent>
            </Dialog>
        </div>
    );
}

function percentToTone(percent: number) {
    if (percent <= 0) return 'bg-slate-200';
    if (percent >= 65) return 'bg-emerald-500';
    if (percent >= 40) return 'bg-amber-500';
    return 'bg-rose-500';
}
