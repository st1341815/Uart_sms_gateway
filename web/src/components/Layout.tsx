import { useEffect, useMemo, useState } from 'react';
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import {
    Clock3,
    LayoutDashboard,
    LogOut,
    Menu,
    MessageSquareText,
    Send,
    Settings,
    Smartphone,
    Webhook,
    Bell,
    X,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { APP_REPO_URL, APP_VERSION } from '@/lib/version.ts';
import { getAllDevicesStatus } from '@/api/serial.ts';
import { getConversations } from '@/api/messages.ts';
import type { AllDevicesStatusResponse, Conversation } from '@/api/types.ts';
import { ThemeToggle } from '@/components/ThemeToggle';
import { cn } from '@/lib/utils.ts';

/**
 * 布局：56px 图标栏（rail）+ 顶栏读数带 + 内容区。
 *
 * 为什么从 248px 宽侧栏改成图标栏：
 *   侧栏常驻占掉 248px，而 7 个页面里有 4 个是「看数据」而不是「点菜单」。
 *   驾驶舱的核心指标（在线 / 今日 / 未读 / 主卡信号）必须每页都在，所以搬到顶栏；
 *   导航降级成图标 + hover 中文，用宽度换信息密度。
 *
 * 版本号放在 rail 最底部竖排：56px 宽横排放不下，
 * 竖排是 IDE 侧栏的既有心智，点击进仓库。
 */

// 一级导航（账号安全收进 rail 底部的齿轮）
const mainNav = [
    { name: '概览', href: '/', icon: LayoutDashboard },
    { name: '短信中心', href: '/messages', icon: MessageSquareText },
    { name: '串口控制', href: '/serial', icon: Smartphone },
    { name: '通知渠道', href: '/notifications', icon: Bell },
    { name: '计划任务', href: '/scheduled-tasks', icon: Clock3 },
    { name: '开放接口', href: '/openapi', icon: Webhook },
] as const;

const accountNav = { name: '账号安全', href: '/account', icon: Settings } as const;

/** 信号等级 0-31 → 百分比 */
const signalPercent = (signal?: number) => {
    if (!signal || signal <= 0) return 0;
    return Math.max(0, Math.min(100, Math.round((signal / 31) * 100)));
};

function RailItem({
    href,
    name,
    icon: Icon,
    active,
    onClick,
    badge,
}: {
    href: string;
    name: string;
    icon: typeof LayoutDashboard;
    active: boolean;
    onClick?: () => void;
    badge?: number;
}) {
    return (
        <div className="group relative">
            <NavLink
                to={href}
                onClick={onClick}
                aria-current={active ? 'page' : undefined}
                aria-label={name}
                className={cn(
                    'relative flex size-10 items-center justify-center rounded-md transition-colors duration-150',
                    active ? 'bg-brand-soft text-brand' : 'text-fg-3 hover:bg-surface-2 hover:text-fg-2',
                )}
            >
                <Icon className="size-[19px]" strokeWidth={1.6} />
                {active && (
                    <span className="absolute left-0 top-2.5 bottom-2.5 w-0.5 rounded-full bg-brand" aria-hidden/>
                )}
                {badge ? (
                    <span className="absolute right-1.5 top-1.5 size-1.5 rounded-full bg-err" aria-hidden/>
                ) : null}
            </NavLink>
            <span
                className="pointer-events-none absolute left-full top-1/2 z-50 ml-2 -translate-y-1/2 whitespace-nowrap rounded border border-line bg-surface px-2 py-1 text-xs text-fg opacity-0 transition-opacity duration-150 group-hover:opacity-100"
                role="tooltip"
            >
                {name}
            </span>
        </div>
    );
}

export default function Layout() {
    const location = useLocation();
    const navigate = useNavigate();
    const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

    const { data: devicesResponse } = useQuery<AllDevicesStatusResponse>({
        queryKey: ['allDevicesStatus'],
        queryFn: getAllDevicesStatus,
        refetchInterval: 10000,
    });

    // 未读数：顶栏读数带要在任何页面都准，所以在这里也拉一次会话列表。
    // 30s 轮询即可 —— 它只是「有几条没读」的提示，真正的实时刷新在短信中心页（5s）。
    const { data: conversations = [] } = useQuery<Conversation[]>({
        queryKey: ['conversations'],
        queryFn: getConversations,
        refetchInterval: 30000,
    });

    const devices = useMemo(() => devicesResponse?.devices ?? [], [devicesResponse]);
    const onlineDevices = devices.filter((device) => device.connected);
    const onlineCount = onlineDevices.length;
    const totalCount = devices.length;

    const unreadCount = useMemo(
        () => conversations.reduce((sum, conversation) => sum + (conversation.unreadCount || 0), 0),
        [conversations],
    );

    const leadDevice = onlineDevices[0];

    // CSQ(0-31) → dBm：3GPP 27.007 经验换算，比直接当 dBm 显示少一个数量级误差

    const leadDbm = leadDevice ? -113 + 2 * (leadDevice.signal ?? 99) : 0;
    const leadSignal = leadDevice ? signalPercent(leadDevice.signal) : 0;
    // 异常 = 离线设备 + 在线但信号 <10/31 的设备
    const abnormalCount = devices.filter(
        (device) => !device.connected || (device.connected && signalPercent(device.signal) < 33),
    ).length;

    const allOnline = totalCount > 0 && onlineCount === totalCount;

    useEffect(() => {
        document.body.style.overflow = mobileMenuOpen ? 'hidden' : '';
        return () => {
            document.body.style.overflow = '';
        };
    }, [mobileMenuOpen]);

    // 移动端抽屉里的导航点击后就关掉，避免遮罩残留（不用 effect 里 setState）
    const closeDrawer = () => setMobileMenuOpen(false);

    const isActive = (href: string) =>
        href === '/' ? location.pathname === '/' : location.pathname.startsWith(href);

    const handleLogout = () => {
        localStorage.removeItem('token');
        localStorage.removeItem('username');
        toast.success('已安全退出');
        navigate('/login');
    };

    const username = (typeof localStorage !== 'undefined' && localStorage.getItem('username')) || 'U';

    const drawer = (
        <div className="flex h-full flex-col bg-rail">
            <div className="flex h-16 items-center gap-2.5 border-b border-line px-4">
                <div className="flex size-8 items-center justify-center rounded-md bg-brand text-sm font-extrabold text-brand-fg">
                    S
                </div>
                <div className="min-w-0">
                    <p className="truncate text-sm font-semibold text-fg">UART 短信网关</p>
                    <p className="num mt-0.5 text-[10px] text-fg-3">v{APP_VERSION}</p>
                </div>
                <button
                    type="button"
                    aria-label="关闭导航"
                    onClick={() => setMobileMenuOpen(false)}
                    className="ml-auto flex size-9 items-center justify-center rounded-md text-fg-2 hover:bg-surface-2 hover:text-fg"
                >
                    <X className="size-5"/>
                </button>
            </div>

            <nav className="flex-1 overflow-y-auto p-2.5" aria-label="主导航">
                {[...mainNav, accountNav].map((item) => {
                    const Icon = item.icon;
                    const active = isActive(item.href);
                    return (
                        <Link
                            key={item.href}
                            to={item.href}
                            onClick={closeDrawer}
                            className={cn(
                                'flex items-center gap-3 rounded-md px-3 py-2.5 text-sm font-medium transition-colors',
                                active
                                    ? 'bg-brand-soft text-brand'
                                    : 'text-fg-2 hover:bg-surface-2 hover:text-fg',
                            )}
                        >
                            <Icon className="size-[18px]" strokeWidth={1.6}/>
                            {item.name}
                        </Link>
                    );
                })}
            </nav>

            <div className="space-y-2 border-t border-line p-3">
                <div className="flex items-center justify-between gap-2">
                    <span className="text-xs text-fg-2">主题</span>
                    <ThemeToggle/>
                </div>
                <button
                    type="button"
                    onClick={handleLogout}
                    className="flex w-full items-center justify-center gap-2 rounded-md border border-line px-2 py-2 text-xs font-medium text-fg-2 transition-colors hover:border-err/40 hover:text-err"
                >
                    <LogOut className="size-3.5"/>
                    退出登录
                </button>
            </div>
        </div>
    );

    return (
        <div className="min-h-screen bg-canvas text-fg">
            {/* ── 桌面图标栏 ── */}
            <aside className="fixed inset-y-0 left-0 z-40 hidden w-14 flex-col items-center border-r border-line bg-rail py-3 lg:flex">
                <Link
                    to="/"
                    aria-label="概览"
                    className="mb-4 flex size-8 items-center justify-center rounded-md bg-brand text-[15px] font-extrabold tracking-tight text-brand-fg"
                >
                    S
                </Link>

                <nav className="flex flex-1 flex-col items-center gap-1" aria-label="主导航">
                    {mainNav.map((item) => (
                        <RailItem
                            key={item.href}
                            href={item.href}
                            name={item.name}
                            icon={item.icon}
                            active={isActive(item.href)}
                            badge={item.href === '/messages' ? unreadCount : undefined}
                        />
                    ))}
                </nav>

                <div className="flex flex-col items-center gap-2">
                    <RailItem
                        href={accountNav.href}
                        name={accountNav.name}
                        icon={accountNav.icon}
                        active={isActive(accountNav.href)}
                    />
                    <a
                        href={APP_REPO_URL}
                        target="_blank"
                        rel="noopener noreferrer"
                        title={`UART SMS Gateway v${APP_VERSION} · 在 GitHub 上查看源码`}
                        className="num mt-1 select-none text-[10px] font-medium leading-none tracking-[0.1em] text-fg-3 transition-colors hover:text-brand [writing-mode:vertical-rl]"
                    >
                        v{APP_VERSION}
                    </a>
                </div>
            </aside>

            {/* ── 移动端抽屉 ── */}
            {mobileMenuOpen && (
                <div className="fixed inset-0 z-50 lg:hidden">
                    <button
                        type="button"
                        aria-label="关闭导航遮罩"
                        onClick={() => setMobileMenuOpen(false)}
                        className="absolute inset-0 bg-black/60"
                    />
                    <aside className="absolute inset-y-0 left-0 w-[min(86vw,300px)] border-r border-line">
                        {drawer}
                    </aside>
                </div>
            )}

            <div className="min-h-screen lg:pl-14">
                {/* ── 顶栏读数带 ── */}
                <header className="sticky top-0 z-30 border-b border-line bg-canvas/95 backdrop-blur-sm">
                    <div className="flex h-16 items-center gap-3 px-3 sm:px-4 lg:px-6">
                        <button
                            type="button"
                            aria-label="打开导航"
                            onClick={() => setMobileMenuOpen(true)}
                            className="flex size-9 shrink-0 items-center justify-center rounded-md border border-line text-fg-2 transition-colors hover:text-fg lg:hidden"
                        >
                            <Menu className="size-[18px]"/>
                        </button>

                        {/* 读数带：桌面显示 5 组，移动端只留在线 + 未读 */}
                        <div className="flex min-w-0 items-center">
                            <div className="flex items-baseline gap-1.5 pr-3 sm:pr-4 lg:pr-5">
                                <span className="hidden text-[11px] tracking-[0.08em] text-fg-2 sm:inline">在线</span>
                                <span className={cn('num text-lg font-semibold sm:text-xl', allOnline ? 'text-ok' : 'text-warn')}>
                                    {onlineCount}
                                </span>
                                <span className="num text-xs text-fg-2">/ {totalCount}</span>
                            </div>

                            <div className="hidden items-baseline gap-1.5 border-l border-line pl-4 pr-5 sm:flex">
                                <span className="text-[11px] tracking-[0.08em] text-fg-2">未读</span>
                                <span className={cn('num text-xl font-semibold', unreadCount > 0 ? 'text-brand' : 'text-fg-2')}>
                                    {unreadCount}
                                </span>
                            </div>

                            <div className="hidden items-baseline gap-1.5 border-l border-line pl-4 pr-5 md:flex">
                                <span className="text-[11px] tracking-[0.08em] text-fg-2">主卡</span>
                                <span className="num text-xl font-semibold text-fg">
                                    {leadDevice ? `−${leadDbm}` : '—'}
                                </span>
                                <span className="num text-xs text-fg-2">
                                    {leadDevice ? `dBm · CSQ ${leadDevice.signal ?? '—'}/31 · ${leadSignal}%` : '等待设备'}
                                </span>
                            </div>

                            <div className="hidden items-baseline gap-1.5 border-l border-line pl-4 pr-5 xl:flex">
                                <span className="text-[11px] tracking-[0.08em] text-fg-2">异常</span>
                                <span className={cn('num text-xl font-semibold', abnormalCount > 0 ? 'text-err' : 'text-fg-2')}>
                                    {abnormalCount}
                                </span>
                            </div>
                        </div>

                        <div className="ml-auto flex items-center gap-2 sm:gap-3">
                            <span className="num hidden text-right text-[10px] leading-[1.5] text-fg-3 md:block">
                                SYNC {new Date().toLocaleTimeString('zh-CN', {hour: '2-digit', minute: '2-digit'})}
                                <br/>
                                每 10s 轮询
                            </span>

                            <ThemeToggle className="hidden sm:inline-flex"/>

                            <Link
                                to="/messages"
                                className="inline-flex h-8 items-center gap-1.5 rounded-md bg-brand px-3 text-xs font-bold text-brand-fg transition-opacity hover:opacity-90"
                            >
                                <Send className="size-3.5"/>
                                发送短信
                            </Link>

                            <div className="flex size-8 items-center justify-center rounded-full border border-line bg-surface-2 text-[11px] font-bold text-fg-2">
                                {username.slice(0, 1).toUpperCase()}
                            </div>
                        </div>
                    </div>
                </header>

                <main
                    className={cn(
                        'app-content mx-auto w-full max-w-[1680px] overflow-x-clip px-3 py-4 sm:px-4 lg:px-6',
                        location.pathname === '/' && 'lg:h-[calc(100vh-64px)] lg:overflow-hidden',
                    )}
                >
                    <Outlet/>
                </main>
            </div>
        </div>
    );
}
