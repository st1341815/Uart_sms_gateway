import {useEffect, useRef, useState} from 'react';
import {ChevronDown, ChevronLeft, Loader2, MoreVertical, Plus, RefreshCw, Search, Send, Trash2, UserRound} from 'lucide-react';
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query';
import {toast} from 'sonner';
import {clearMessages, deleteConversation, deleteMessage, getConversationMessages, getConversations} from '../api/messages';
import {getAllDevicesStatus, sendSMS} from '../api/serial';
import type {AllDevicesStatusResponse, Conversation, DeviceStatusSummary, TextMessage} from '@/api/types';
import {Button} from '@/components/ui/button';
import {Input} from '@/components/ui/input';
import {Textarea} from '@/components/ui/textarea';
import {PageHeader} from '@/components/PageHeader';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue} from '@/components/ui/select';
import {cn} from '@/lib/utils';

const AVATAR_TONES = [
    'bg-blue-700',
    'bg-slate-600',
    'bg-blue-600',
    'bg-indigo-600',
    'bg-sky-700',
    'bg-slate-700',
];

const getAvatarTone = (peer: string) => {
    const sum = peer.split('').reduce((acc, char) => acc + char.charCodeAt(0), 0);
    return AVATAR_TONES[sum % AVATAR_TONES.length];
};

const formatTime = (timestamp: number) => {
    if (!timestamp) return '—';
    const date = new Date(timestamp);
    const now = new Date();
    const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    const startOfYesterday = startOfToday - 24 * 60 * 60 * 1000;
    const time = date.toLocaleTimeString('zh-CN', {hour: '2-digit', minute: '2-digit'});

    if (date.getTime() >= startOfToday) return time;
    if (date.getTime() >= startOfYesterday) return `昨天 ${time}`;
    if (date.getFullYear() === now.getFullYear()) {
        return `${date.toLocaleDateString('zh-CN', {month: '2-digit', day: '2-digit'})} ${time}`;
    }
    return date.toLocaleDateString('zh-CN', {year: '2-digit', month: '2-digit', day: '2-digit'});
};

function StatusBadge({status}: {status: TextMessage['status']}) {
    switch (status) {
        case 'sent':
            return <span className="font-semibold text-emerald-600">✓ 已发送</span>;
        case 'failed':
            return <span className="font-semibold text-rose-600">✗ 失败</span>;
        case 'sending':
            return <span className="font-semibold text-slate-400">发送中…</span>;
        default:
            return null;
    }
}

export default function Messages() {
    const queryClient = useQueryClient();
    const messagesEndRef = useRef<HTMLDivElement>(null);

    // 用户手动点选的会话；为空时回落到第一个会话。
    // 这样「默认选中第一个会话」就是纯派生计算，不需要在 effect 里 setState
    // （react-hooks/set-state-in-effect：effect 内同步 setState 会触发级联渲染）。
    // 会话唯一键 = 设备ID + '\u0000' + 对方号码。
    // 同一号码可能同时与两张卡有往来，只按号码选会把两条通信线混淆。
    const convKey = (deviceId: string, peer: string) => `${deviceId}\u0000${peer}`;
    const [peerSelection, setPeerSelection] = useState<string | null>(null);
    const [inputText, setInputText] = useState('');
    // 发送设备（必选）：多卡场景下不能默认，避免从非预期号码（如境外漫游卡）发出
    const [sendDeviceId, setSendDeviceId] = useState<string>('');
    const [searchQuery, setSearchQuery] = useState('');
    // 分组折叠：存「已折叠的设备 ID」。默认全部展开（空集），
    // 用 Set 而不是 map[string]bool，增删都是一处，不用维护多余键。
    // 未知设备（历史数据/已删设备）用 '__unknown__' 当键（deviceId 可能是空串，
    // 空串语义上更接近「没有」，单列一个常量更明确）。
    const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(new Set());
    const [composeOpen, setComposeOpen] = useState(false);
    // 用户手动选择的群发设备；为空时回落到第一台在线设备（同样是派生值）。
    const [composeDeviceSelection, setComposeDeviceSelection] = useState('');
    const [newRecipient, setNewRecipient] = useState('');
    const [newContent, setNewContent] = useState('');

    const {data: devicesResponse} = useQuery<AllDevicesStatusResponse>({
        queryKey: ['allDevicesStatus'],
        queryFn: getAllDevicesStatus,
        refetchInterval: 10000,
    });
    const devices: DeviceStatusSummary[] = devicesResponse?.devices ?? [];
    const onlineDevices = devices.filter((device) => device.connected);

    const {data: conversations = [], isLoading, refetch, isFetching} = useQuery<Conversation[]>({
        queryKey: ['conversations'],
        queryFn: getConversations,
        refetchInterval: 5000,
    });

    // 以下两个都是「派生值 + 可覆盖」写法，替代原先「effect 里 setState 选默认项」：
    //   - 用户点选过 → 用用户的选择（哪怕是空）
    //   - 没点选过 → 回落到第一个会话 / 第一台在线设备
    // react-hooks/set-state-in-effect 规则认为 effect 内同步 setState 会级联渲染。
    // 选中的会话用「设备 + 号码」组合键标识；未选时回落到列表第一条。
    const selectedConvKey = peerSelection
        ?? (conversations[0] ? convKey(conversations[0].deviceId, conversations[0].peer) : null);
    const activeConversation = conversations.find(
        (conv) => convKey(conv.deviceId, conv.peer) === selectedConvKey,
    ) ?? null;
    const selectedPeer = activeConversation?.peer ?? null;
    // 该会话所属的卡：发消息默认从这张卡发出，避免回错卡
    const selectedConvDeviceId = activeConversation?.deviceId ?? '';
    const composeDeviceId = composeDeviceSelection || onlineDevices[0]?.id || '';

    const {data: currentMessages = []} = useQuery<TextMessage[]>({
        queryKey: ['conversation-messages', selectedConvKey],
        queryFn: () => (selectedPeer
            ? getConversationMessages(selectedPeer, selectedConvDeviceId)
            : Promise.resolve([])),
        enabled: Boolean(selectedPeer),
        refetchInterval: 5000,
    });

    const refreshAll = () => {
        queryClient.invalidateQueries({queryKey: ['conversations']});
        queryClient.invalidateQueries({queryKey: ['conversation-messages']});
    };

    const sendMutation = useMutation({
        mutationFn: (payload: {to: string; content: string; device_id: string}) => sendSMS(payload),
        onError: (error: unknown) => {
            console.error('发送失败:', error);
            toast.error(error instanceof Error ? error.message : '发送失败');
        },
    });

    const clearMutation = useMutation({
        mutationFn: clearMessages,
        onSuccess: () => {
            toast.success('已清空全部短信');
            setPeerSelection(null);
            refreshAll();
        },
        onError: () => toast.error('清空失败'),
    });

    const deleteConversationMutation = useMutation({
        // 带 deviceId：只删这张卡与对方的会话，不波及另一张卡的同名会话
        mutationFn: (target: {peer: string; deviceId: string}) => deleteConversation(target.peer, target.deviceId),
        onSuccess: (_, target) => {
            toast.success('会话已删除');
            if (selectedConvKey === convKey(target.deviceId, target.peer)) setPeerSelection(null);
            queryClient.invalidateQueries({queryKey: ['conversations']});
        },
        onError: () => toast.error('删除会话失败'),
    });

    const deleteMessageMutation = useMutation({
        mutationFn: (id: string) => deleteMessage(id),
        onSuccess: () => {
            toast.success('消息已删除');
            refreshAll();
        },
        onError: () => toast.error('删除消息失败'),
    });

    useEffect(() => {
        messagesEndRef.current?.scrollIntoView({behavior: 'smooth'});
    }, [selectedPeer, currentMessages]);

    const keyword = searchQuery.trim().toLowerCase();
    const filteredConversations = conversations.filter((conv) =>
        conv.peer.toLowerCase().includes(keyword) ||
        conv.lastMessage.content.toLowerCase().includes(keyword),
    );

    // 按设备分组：conversations 后端已按最后消息时间倒序，
    // 这里保持组内原有顺序，只把同一张卡的会话聚到一起。
    // 分组顺序按 devices 的配置顺序（order 已由后端保证），
    // 未在 devices 里出现的 deviceId（历史数据 / 已删设备）统一落到「其他」。
    const groupedConversations: {deviceId: string; items: Conversation[]}[] = [];
    const groupIndex = new Map<string, number>();
    for (const conv of filteredConversations) {
        const gid = conv.deviceId || '';
        let idx = groupIndex.get(gid);
        if (idx === undefined) {
            idx = groupedConversations.length;
            groupIndex.set(gid, idx);
            groupedConversations.push({deviceId: gid, items: []});
        }
        groupedConversations[idx].items.push(conv);
    }
    // 按 devices 的配置顺序重排分组（在线与否不影响），未知设备排最后
    groupedConversations.sort((a, b) => {
        const ia = devices.findIndex((d) => d.id === a.deviceId);
        const ib = devices.findIndex((d) => d.id === b.deviceId);
        const ra = ia === -1 ? Number.MAX_SAFE_INTEGER : ia;
        const rb = ib === -1 ? Number.MAX_SAFE_INTEGER : ib;
        return ra - rb;
    });

    // 分组键：deviceId 可能是空串（历史数据），统一映射成常量，避免与真实 ID 撞键
    const groupKeyOf = (deviceId: string) => deviceId || '__unknown__';

    const toggleGroup = (deviceId: string) => {
        const key = groupKeyOf(deviceId);
        setCollapsedGroups((prev) => {
            const next = new Set(prev);
            if (next.has(key)) {
                next.delete(key);
            } else {
                next.add(key);
            }
            return next;
        });
    };

    const allCollapsed = groupedConversations.length > 0
        && groupedConversations.every((g) => collapsedGroups.has(groupKeyOf(g.deviceId)));

    const toggleAllGroups = () => {
        if (allCollapsed) {
            setCollapsedGroups(new Set());
        } else {
            setCollapsedGroups(new Set(groupedConversations.map((g) => groupKeyOf(g.deviceId))));
        }
    };

    const handleSend = (event: React.FormEvent) => {
        event.preventDefault();
        if (!selectedPeer || !inputText.trim()) {
            toast.warning('请输入短信内容');
            return;
        }
        // 设备必选：短信必须明确从哪张卡发出，避免误用境外漫游卡产生国际资费。
        // 未手选时回落到当前会话所属的卡（见 effectiveSendDeviceId）。
        if (!effectiveSendDeviceId) {
            toast.warning('请先选择发送设备');
            return;
        }
        const device = devices.find((d) => d.id === effectiveSendDeviceId);
        sendMutation.mutate(
            {to: selectedPeer, content: inputText.trim(), device_id: effectiveSendDeviceId},
            {
                onSuccess: () => {
                    setInputText('');
                    toast.success(`已提交发送${device ? `（${device.name}）` : ''}`);
                    refreshAll();
                },
            },
        );
    };

    const handleComposeSubmit = (event: React.FormEvent) => {
        event.preventDefault();
        const recipient = newRecipient.trim();
        const message = newContent.trim();
        if (!recipient || !message) {
            toast.warning('请输入目标号码和短信内容');
            return;
        }
        if (!composeDeviceId) {
            toast.warning('请选择发送设备');
            return;
        }
        sendMutation.mutate(
            {to: recipient, content: message, device_id: composeDeviceId},
            {
                onSuccess: () => {
                    toast.success('短信已提交发送');
                    setComposeOpen(false);
                    setNewRecipient('');
                    setNewContent('');
                    // 选中「刚发出的那张卡 + 收件人」这条会话
                    setPeerSelection(convKey(composeDeviceId, recipient));
                    refreshAll();
                },
            },
        );
    };

    const handleDeleteConversation = () => {
        if (!selectedPeer) return;
        const cardLabel = selectedConvDeviceId
            ? (devices.find((d) => d.id === selectedConvDeviceId)?.name || selectedConvDeviceId)
            : '该设备';
        if (!window.confirm(`确定删除「${cardLabel}」与 ${selectedPeer} 的全部消息吗？此操作不可恢复。`)) return;
        deleteConversationMutation.mutate({peer: selectedPeer, deviceId: selectedConvDeviceId});
    };

    const handleDeleteMessage = (id: string, event: React.MouseEvent) => {
        event.stopPropagation();
        if (!window.confirm('确定删除这条消息吗？')) return;
        deleteMessageMutation.mutate(id);
    };

    const handleClear = () => {
        if (!window.confirm('确定清空全部短信吗？此操作不可恢复。')) return;
        clearMutation.mutate();
    };

    if (isLoading) {
        return (
            <div className="flex min-h-[60vh] items-center justify-center gap-2 text-sm text-slate-500">
                <Loader2 className="size-4 animate-spin text-blue-600"/>
                正在读取会话
            </div>
        );
    }

    // 实际用于发送的卡：用户手动选过就用他的选择，否则跟随当前会话所属的卡。
    // 这样「打开哪张卡的会话，回复就从哪张卡发出」，不会因忘选而回错卡。
    const effectiveSendDeviceId = sendDeviceId || selectedConvDeviceId;
    const sendingVia = devices.find((d) => d.id === effectiveSendDeviceId);

    // 把消息上的 deviceId 渲染成可读名称
    const deviceLabel = (deviceId: string) => {
        const d = devices.find((x) => x.id === deviceId);
        return d?.name || deviceId;
    };

    return (
        <div className="flex h-[calc(100dvh-108px)] min-h-[560px] flex-col">
            <PageHeader
                title="短信中心"
                description="查看全部设备的收发记录，或在指定 SIM 卡上发送新短信。"
                action={<div className="flex gap-2">
                    <Button
                        onClick={() => setComposeOpen(true)}
                        size="sm"
                        className="bg-blue-600 text-white hover:bg-blue-700"
                    >
                        <Plus className="mr-2 size-4"/>
                        新建短信
                    </Button>
                    <Button
                        onClick={() => refetch()}
                        variant="outline"
                        size="sm"
                        className="hover:bg-slate-50"
                        title="刷新会话列表"
                    >
                        <RefreshCw className={cn('size-4', isFetching && 'animate-spin')}/>
                        <span className="ml-2 hidden sm:inline">刷新</span>
                    </Button>
                    <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                            <Button variant="outline" size="sm" className="hover:bg-slate-50">
                                <MoreVertical className="size-4"/>
                            </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                            <DropdownMenuItem
                                onClick={handleClear}
                                className="cursor-pointer text-rose-600 focus:bg-rose-50 focus:text-rose-700"
                            >
                                <Trash2 className="mr-2 size-4"/>
                                清空所有短信
                            </DropdownMenuItem>
                        </DropdownMenuContent>
                    </DropdownMenu>
                </div>}
            />

            <div className="mt-6 flex min-h-0 flex-1 overflow-hidden rounded-2xl border border-slate-200 bg-white">
                {/* 会话列表 */}
                <div className={cn(
                    'w-full flex-col border-slate-200 md:flex md:w-[300px] md:border-r xl:w-[330px]',
                    selectedPeer ? 'hidden' : 'flex',
                )}>
                    <div className="border-b border-slate-100 p-4">
                        <div className="relative">
                            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-slate-400"/>
                            <Input
                                type="text"
                                placeholder="搜索号码或内容..."
                                value={searchQuery}
                                onChange={(event) => setSearchQuery(event.target.value)}
                                className="h-9 border-transparent bg-slate-100/70 pl-9 pr-3 focus-visible:border-blue-500 focus-visible:bg-white"
                            />
                        </div>
                        <div className="mt-2 flex items-center justify-between px-1">
                            <span className="text-[11px] text-slate-400">
                                {groupedConversations.length > 0 && `${groupedConversations.length} 个号码 · ${filteredConversations.length} 个会话`}
                            </span>
                            {groupedConversations.length > 1 && (
                                <button
                                    type="button"
                                    onClick={toggleAllGroups}
                                    className="rounded px-1.5 py-0.5 text-[11px] font-medium text-blue-600 transition-colors hover:bg-blue-50"
                                >
                                    {allCollapsed ? '全部展开' : '全部折叠'}
                                </button>
                            )}
                        </div>
                    </div>

                    <div className="flex-1 overflow-y-auto">
                        {filteredConversations.length === 0 ? (
                            <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center text-slate-400">
                                <UserRound className="size-10 opacity-25"/>
                                <p className="text-sm font-medium">{conversations.length === 0 ? '暂无会话' : '没有匹配的会话'}</p>
                                <p className="text-xs">{conversations.length === 0 ? '收到或发送第一条短信后会自动创建会话' : '换个关键词试试'}</p>
                            </div>
                        ) : (
                            groupedConversations.map((group) => {
                                const device = devices.find((d) => d.id === group.deviceId);
                                return (
                                    <div key={group.deviceId || '__unknown__'}>
                                        {/* 分组头：这张卡的名称 + 号码，一眼看出是哪个号在收发。
                                            整行可点，点击折叠/展开该组；sticky 保证长列表滚动时分组名常驻。 */}
                                        <button
                                            type="button"
                                            onClick={() => toggleGroup(group.deviceId)}
                                            aria-expanded={!collapsedGroups.has(groupKeyOf(group.deviceId))}
                                            className="sticky top-0 z-10 flex w-full items-center gap-2 border-b border-slate-100 bg-slate-50/95 px-3 py-2 text-left backdrop-blur transition-colors hover:bg-slate-100"
                                        >
                                            <ChevronDown
                                                className={cn(
                                                    'size-3.5 shrink-0 text-slate-400 transition-transform duration-150',
                                                    collapsedGroups.has(groupKeyOf(group.deviceId)) && '-rotate-90',
                                                )}
                                            />
                                            <span className="truncate text-[11px] font-bold text-slate-600">
                                                {group.deviceId ? (device?.name || group.deviceId) : '未标记设备'}
                                            </span>
                                            {device?.phoneNumber && (
                                                <span className="shrink-0 font-mono text-[10px] text-slate-400">
                                                    {device.phoneNumber}
                                                </span>
                                            )}
                                            {device && !device.connected && (
                                                <span className="shrink-0 rounded bg-slate-200 px-1 text-[9px] text-slate-500">离线</span>
                                            )}
                                            <span className="ml-auto shrink-0 rounded-full bg-slate-200/70 px-1.5 text-[10px] font-medium text-slate-500">
                                                {group.items.length}
                                            </span>
                                        </button>
                                        {!collapsedGroups.has(groupKeyOf(group.deviceId)) && group.items.map((conv) => {
                                            const active = selectedConvKey === convKey(conv.deviceId, conv.peer);
                                            return (
                                                <button
                                                    key={convKey(conv.deviceId, conv.peer)}
                                                    type="button"
                                                    onClick={() => setPeerSelection(convKey(conv.deviceId, conv.peer))}
                                                    className={cn(
                                                        'flex w-full items-start gap-2.5 border-l-2 px-4 py-3.5 text-left transition-colors',
                                                        active ? 'border-blue-600 bg-blue-50/60' : 'border-transparent hover:bg-slate-50',
                                                    )}
                                                >
                                                    <span className={cn(
                                                        'flex size-9 shrink-0 items-center justify-center rounded-full text-xs font-bold text-white',
                                                        getAvatarTone(conv.peer),
                                                    )}>
                                                        {conv.peer.slice(-2)}
                                                    </span>
                                                    <span className="min-w-0 flex-1">
                                                        <span className="flex items-baseline justify-between gap-2">
                                                            <span className={cn('truncate text-sm font-semibold', active ? 'text-slate-950' : 'text-slate-700')}>
                                                                {conv.peer}
                                                            </span>
                                                            <span className="shrink-0 text-[10px] text-slate-400">
                                                                {formatTime(conv.lastMessage.createdAt)}
                                                            </span>
                                                        </span>
                                                        <span className="mt-0.5 line-clamp-1 text-xs text-slate-500">
                                                            {conv.lastMessage.type === 'outgoing' && '我: '}
                                                            {conv.lastMessage.content}
                                                        </span>
                                                    </span>
                                                </button>
                                            );
                                        })}
                                        {/* 折叠态给一行提示，避免分组头下面空一块看不明白 */}
                                        {collapsedGroups.has(groupKeyOf(group.deviceId)) && (
                                            <div className="px-4 pb-2 pt-0.5 text-[10px] text-slate-400">
                                                已折叠 {group.items.length} 个会话
                                            </div>
                                        )}
                                    </div>
                                );
                            })
                        )}
                    </div>
                </div>

                {/* 会话详情 */}
                <div className={cn(
                    'flex-1 flex-col bg-slate-50/40',
                    selectedPeer ? 'flex' : 'hidden md:flex',
                )}>
                    <div className="flex h-15 shrink-0 items-center justify-between gap-3 border-b border-slate-200 bg-white px-4 md:px-6">
                        {selectedPeer ? (
                            <>
                                <div className="flex min-w-0 items-center gap-3">
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        onClick={() => setPeerSelection(null)}
                                        className="-ml-2 text-slate-600 md:hidden"
                                        aria-label="返回会话列表"
                                    >
                                        <ChevronLeft className="size-5"/>
                                    </Button>
                                    <span className={cn(
                                        'flex size-10 shrink-0 items-center justify-center rounded-full text-sm font-bold text-white',
                                        getAvatarTone(selectedPeer),
                                    )}>
                                        {selectedPeer.slice(-2)}
                                    </span>
                                    <div className="min-w-0">
                                        <h3 className="truncate text-sm font-bold text-slate-900">{selectedPeer}</h3>
                                        <span className="text-xs text-slate-500">
                                            共 {activeConversation?.messageCount ?? 0} 条消息
                                        </span>
                                    </div>
                                </div>
                                <DropdownMenu>
                                    <DropdownMenuTrigger asChild>
                                        <Button variant="ghost" size="sm" className="text-slate-400 hover:text-slate-600" aria-label="会话操作">
                                            <MoreVertical className="size-4"/>
                                        </Button>
                                    </DropdownMenuTrigger>
                                    <DropdownMenuContent align="end">
                                        <DropdownMenuItem
                                            onClick={handleDeleteConversation}
                                            className="cursor-pointer text-rose-600 focus:bg-rose-50 focus:text-rose-700"
                                        >
                                            <Trash2 className="mr-2 size-4"/>
                                            删除会话
                                        </DropdownMenuItem>
                                    </DropdownMenuContent>
                                </DropdownMenu>
                            </>
                        ) : (
                            <span className="text-sm text-slate-400">请选择会话</span>
                        )}
                    </div>

                    <div className="flex-1 overflow-y-auto p-4 md:p-6">
                        {selectedPeer && currentMessages.length > 0 ? (
                            <div className="mx-auto w-full max-w-[980px] space-y-4">
                                {currentMessages.map((msg) => {
                                    const outgoing = msg.type === 'outgoing';
                                    return (
                                        <div
                                            key={msg.id}
                                            className={cn('group flex animate-in fade-in slide-in-from-bottom-2 duration-200', outgoing ? 'justify-end' : 'justify-start')}
                                        >
                                            <div className={cn('relative flex max-w-[82%] flex-col sm:max-w-[75%] xl:max-w-[68%]', outgoing ? 'items-end' : 'items-start')}>
                                                <div className={cn(
                                                    'relative rounded-2xl px-4 py-2.5 text-sm leading-relaxed',
                                                    outgoing
                                                        ? 'rounded-tr-sm bg-[#0b2a55] text-white'
                                                        : 'rounded-tl-sm border border-slate-200 bg-white text-slate-800',
                                                )}>
                                                    <p className="whitespace-pre-wrap break-words">{msg.content}</p>
                                                    <button
                                                        type="button"
                                                        onClick={(event) => handleDeleteMessage(msg.id, event)}
                                                        className={cn(
                                                            'absolute -top-2 rounded-full border border-rose-200 bg-white p-1 text-rose-500 opacity-0 transition-opacity hover:bg-rose-50 group-hover:opacity-100 focus-visible:opacity-100',
                                                            outgoing ? '-left-2' : '-right-2',
                                                        )}
                                                        title="删除消息"
                                                        aria-label="删除消息"
                                                    >
                                                        <Trash2 className="size-3"/>
                                                    </button>
                                                </div>
                                                <div className={cn(
                                                    'mt-1 flex items-center gap-2 px-1',
                                                    outgoing && 'flex-row-reverse',
                                                )}>
                                                    <span className="text-[10px] text-slate-400">{formatTime(msg.createdAt)}</span>
                                                    {msg.deviceId && (
                                                        <span
                                                            className={cn(
                                                                'rounded border px-1.5 py-0.5 text-[10px]',
                                                                outgoing
                                                                    ? 'border-blue-200 bg-blue-50 text-blue-700'
                                                                    : 'border-slate-200 bg-slate-50 text-slate-600',
                                                            )}
                                                            title={`来源设备: ${deviceLabel(msg.deviceId)}`}
                                                        >
                                                            {deviceLabel(msg.deviceId)}
                                                        </span>
                                                    )}
                                                    {outgoing && <StatusBadge status={msg.status}/>}
                                                </div>
                                            </div>
                                        </div>
                                    );
                                })}
                                <div ref={messagesEndRef}/>
                            </div>
                        ) : (
                            <div className="flex h-full flex-col items-center justify-center gap-2 text-center text-slate-400">
                                <Send className="size-10 opacity-20"/>
                                <p className="text-sm font-medium">
                                    {selectedPeer ? '暂无消息' : '选择左侧会话开始查看'}
                                </p>
                                <p className="text-xs">
                                    {selectedPeer ? '在下方输入框发送第一条短信' : '或点击右上角「新建短信」主动发起'}
                                </p>
                            </div>
                        )}
                    </div>

                    <div className="border-t border-slate-200 bg-white p-4">
                        <form className="flex gap-3" onSubmit={handleSend}>
                            {/* 发送设备：必选，多卡场景避免从非预期号码发出 */}
                            <select
                                value={effectiveSendDeviceId}
                                onChange={(event) => setSendDeviceId(event.target.value)}
                                disabled={sendMutation.isPending}
                                title="选择从哪张卡发送"
                                aria-label="发送设备"
                                className={cn(
                                    'h-10 shrink-0 rounded-lg border px-2 text-sm outline-none transition-colors',
                                    effectiveSendDeviceId
                                        ? 'border-slate-200 bg-slate-50 text-slate-700 focus:border-blue-500'
                                        : 'border-amber-300 bg-amber-50 text-amber-700',
                                )}
                            >
                                <option value="">选择设备</option>
                                {devices.map((device) => (
                                    <option key={device.id} value={device.id}>
                                        {device.name || device.id}
                                        {device.connected ? '' : '（离线）'}
                                    </option>
                                ))}
                            </select>
                            <Input
                                type="text"
                                value={inputText}
                                onChange={(event) => setInputText(event.target.value)}
                                placeholder={
                                    !selectedPeer
                                        ? '请先选择会话'
                                        : !effectiveSendDeviceId
                                            ? '请先选择发送设备'
                                            : `通过 ${sendingVia?.name ?? effectiveSendDeviceId} 发送...`
                                }
                                disabled={!selectedPeer || sendMutation.isPending}
                                className="h-10 flex-1 border-slate-200 bg-slate-50 focus-visible:border-blue-500 focus-visible:bg-white"
                            />
                            <Button
                                type="submit"
                                disabled={!selectedPeer || !effectiveSendDeviceId || !inputText.trim() || sendMutation.isPending}
                                className="h-10 bg-[#0b2a55] px-6 text-white hover:bg-slate-800"
                            >
                                {sendMutation.isPending ? (
                                    <Loader2 className="size-4 animate-spin"/>
                                ) : (
                                    <>
                                        <Send className="mr-2 size-4"/>
                                        发送
                                    </>
                                )}
                            </Button>
                        </form>
                        {selectedPeer && (
                            <p className="mt-2 text-[11px] text-slate-400">
                                {sendingVia
                                    ? <>回复将从 <span className="font-medium text-slate-600">{sendingVia.name}</span> 送出。</>
                                    : <>请先在左侧下拉中选择发送设备（多卡场景必须明确来源，避免误用漫游卡）。</>}
                            </p>
                        )}
                    </div>
                </div>
            </div>

            {/* 新建短信 */}
            <Dialog open={composeOpen} onOpenChange={setComposeOpen}>
                <DialogContent className="sm:max-w-lg">
                    <form onSubmit={handleComposeSubmit}>
                        <DialogHeader>
                            <DialogTitle>新建短信</DialogTitle>
                            <DialogDescription>选择一张 SIM 卡，向任意号码发送短信。</DialogDescription>
                        </DialogHeader>

                        <div className="space-y-5 py-5">
                            <div className="space-y-1.5">
                                <label className="block text-sm font-medium text-slate-800">发送设备</label>
                                <Select value={composeDeviceId} onValueChange={setComposeDeviceSelection}>
                                    <SelectTrigger className="h-10 w-full border-slate-200 bg-slate-50">
                                        <SelectValue placeholder="选择设备"/>
                                    </SelectTrigger>
                                    <SelectContent>
                                        {devices.length === 0 ? (
                                            <div className="px-3 py-2 text-xs text-slate-400">尚未配置设备</div>
                                        ) : devices.map((device) => (
                                            <SelectItem key={device.id} value={device.id}>
                                                <span className="flex items-center gap-2">
                                                    <span className={cn('size-1.5 rounded-full', device.connected ? 'bg-emerald-500' : 'bg-slate-300')}/>
                                                    <span className="font-medium">{device.name}</span>
                                                    <span className="font-mono text-[10px] text-slate-400">{device.operator || '未注册'}</span>
                                                </span>
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </div>

                            <div className="space-y-1.5">
                                <label htmlFor="compose-recipient" className="block text-sm font-medium text-slate-800">目标号码</label>
                                <Input
                                    id="compose-recipient"
                                    type="tel"
                                    value={newRecipient}
                                    onChange={(event) => setNewRecipient(event.target.value)}
                                    placeholder="请输入手机号"
                                    autoComplete="tel"
                                    className="bg-slate-50 font-mono focus-visible:bg-white"
                                    autoFocus
                                />
                            </div>

                            <div className="space-y-1.5">
                                <div className="flex items-center justify-between">
                                    <label htmlFor="compose-content" className="block text-sm font-medium text-slate-800">短信内容</label>
                                    <span className="text-xs text-slate-400">{newContent.length} 字</span>
                                </div>
                                <Textarea
                                    id="compose-content"
                                    value={newContent}
                                    onChange={(event) => setNewContent(event.target.value)}
                                    placeholder="请输入短信内容"
                                    className="min-h-32 resize-none bg-slate-50 focus-visible:bg-white"
                                />
                            </div>

                            {onlineDevices.length === 0 && (
                                <p className="rounded-lg border border-rose-200 bg-rose-50 px-3.5 py-3 text-xs leading-5 text-rose-700">
                                    当前没有在线设备，串口连接成功后才能发送短信。
                                </p>
                            )}
                        </div>

                        <DialogFooter>
                            <Button type="button" variant="outline" onClick={() => setComposeOpen(false)}>
                                取消
                            </Button>
                            <Button
                                type="submit"
                                disabled={onlineDevices.length === 0 || !newRecipient.trim() || !newContent.trim() || sendMutation.isPending}
                                className="bg-blue-600 text-white hover:bg-blue-700"
                            >
                                {sendMutation.isPending ? <Loader2 className="size-4 animate-spin"/> : <Send className="size-4"/>}
                                {sendMutation.isPending ? '发送中...' : '发送短信'}
                            </Button>
                        </DialogFooter>
                    </form>
                </DialogContent>
            </Dialog>
        </div>
    );
}
