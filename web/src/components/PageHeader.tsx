import type {ReactNode} from 'react';

interface PageHeaderProps {
    title: string;
    description: string;
    action?: ReactNode;
}

/**
 * 页面标题。
 *
 * 方案 A 之后这层刻意做「薄」：标题 22px、描述 12.5px 次色，
 * 不再用 28px 大字压住整个首屏 —— 驾驶舱里信息本身才是主角，
 * 标题只负责定位，不抢占视觉权重。
 */
export function PageHeader({title, description, action}: PageHeaderProps) {
    return (
        <header className="flex flex-col justify-between gap-3 sm:flex-row sm:items-end">
            <div className="min-w-0">
                <h1 className="text-[22px] font-bold tracking-[-0.02em] text-fg">{title}</h1>
                <p className="mt-1 max-w-2xl text-[12.5px] leading-5 text-fg-3">{description}</p>
            </div>
            {action && <div className="shrink-0">{action}</div>}
        </header>
    );
}
