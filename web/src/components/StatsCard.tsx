import React from 'react';
import {cn} from '@/lib/utils.ts';

export interface Props {
    label: string;
    value?: number | string;
    icon: React.FC<{size: number} & React.SVGProps<SVGSVGElement>>;
    unit?: string;
    subValue?: string;
    colorClass: string;
    /** 次级信息是否用等宽字体（串口路径、号码等） */
    monoSub?: boolean;
}

export const StatCard = ({label, value, icon: Icon, unit, subValue, colorClass, monoSub = false}: Props) => (
    <div className="relative flex h-full flex-col overflow-hidden rounded-md border border-line bg-surface p-4 transition-colors duration-200 hover:border-brand/30">
        <div className="flex items-start justify-between gap-4">
            <div className="min-w-0">
                <p className="text-xs font-semibold uppercase tracking-[0.11em] text-fg-3">{label}</p>
                <div className="mt-2 flex items-baseline gap-1.5">
                    <span className="truncate text-2xl font-bold tracking-[-0.04em] text-fg">{value ?? '—'}</span>
                    {unit && <span className="text-sm font-semibold text-fg-3">{unit}</span>}
                </div>
            </div>
            <div className={cn('flex size-10 shrink-0 items-center justify-center rounded-md', colorClass)}>
                <Icon size={20}/>
            </div>
        </div>
        <div className="mt-auto min-h-5 border-t border-line-soft pt-2.5">
            <p className={cn('truncate text-xs text-fg-3', monoSub && 'font-mono')}>
                {subValue || '实时数据已同步'}
            </p>
        </div>
    </div>
);
