import {useCallback, useSyncExternalStore} from 'react';

/**
 * 订阅 CSS 媒体查询，返回当前是否命中。
 *
 * 用 useSyncExternalStore 而不是 useState + useEffect：
 *   1. effect 里同步 setState 会触发 react-hooks/set-state-in-effect 告警
 *      （本项目 Messages.tsx 里已有同类注释说明过这个约束）；
 *   2. matchMedia 属于外部可变源，订阅式读取是它语义上正确的用法，
 *      不会出现首帧渲染出错误值、下一帧再纠正的闪烁。
 */
export function useMediaQuery(query: string): boolean {
    const subscribe = useCallback((onStoreChange: () => void) => {
        const mql = window.matchMedia(query);
        mql.addEventListener('change', onStoreChange);
        return () => mql.removeEventListener('change', onStoreChange);
    }, [query]);

    return useSyncExternalStore(
        subscribe,
        () => window.matchMedia(query).matches,
        // SSR / 无 window 环境下的兜底值。本项目是纯客户端 Vite 构建，
        // 走不到这里，但给出确定值可以避免 hook 在测试环境里报错。
        () => false,
    );
}
