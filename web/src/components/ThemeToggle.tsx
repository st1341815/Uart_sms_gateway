import { useTheme } from 'next-themes';
import { Monitor, Moon, Sun } from 'lucide-react';
import { cn } from '@/lib/utils';

/**
 * 三态主题切换：亮色 / 跟随系统 / 深色。
 *
 * 为什么不做成「点一下循环」：三态循环没有可预期的下一站，
 * 用户点两次才回到原位很烦；分段控件一眼能看到全部选项，点哪到哪。
 *
 * 为什么不需要 mounted 守卫：本项目是纯 CSR（无 SSR），
 * next-themes 在首帧给的是 undefined，这里 fallback 到 'dark' 与
 * defaultTheme 一致，所以不会出现「先亮后暗」的闪烁。
 */
const OPTIONS = [
  { value: 'light', label: '亮色', icon: Sun },
  { value: 'system', label: '跟随系统', icon: Monitor },
  { value: 'dark', label: '深色', icon: Moon },
] as const;

export function ThemeToggle({ className }: { className?: string }) {
  const { theme, setTheme } = useTheme();
  const current = theme ?? 'dark';

  return (
    <div
      role="group"
      aria-label="主题模式"
      className={cn(
        'inline-flex items-center gap-0.5 rounded-md border border-line bg-surface-3 p-0.5',
        className,
      )}
    >
      {OPTIONS.map((option) => {
        const Icon = option.icon;
        const active = current === option.value;
        return (
          <button
            key={option.value}
            type="button"
            onClick={() => setTheme(option.value)}
            title={option.label}
            aria-label={option.label}
            aria-pressed={active}
            className={cn(
              'flex size-7 items-center justify-center rounded-[3px] transition-colors duration-150',
              'focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-brand',
              active
                ? 'bg-brand-soft text-brand'
                : 'text-fg-3 hover:text-fg-2',
            )}
          >
            <Icon className="size-4" strokeWidth={1.75} />
          </button>
        );
      })}
    </div>
  );
}