import { useEffect, useRef, useState } from 'react';

/**
 * Whether a scrolling block has more below its bottom edge. The edge then
 * gets a line, so the content reads as going on under the buttons rather
 * than cut off. `active` is false while the block is not on screen yet.
 */
export function useMoreBelow<T extends HTMLElement>(active: boolean) {
    const ref = useRef<T>(null);
    const [moreBelow, setMoreBelow] = useState(false);

    useEffect(() => {
        const el = ref.current;
        if (!active || !el) {
            return;
        }
        const check = () => setMoreBelow(el.scrollHeight - el.scrollTop - el.clientHeight > 1);
        check();
        el.addEventListener('scroll', check, { passive: true });
        // The block and what is in it change size as the data comes in.
        const observer = new ResizeObserver(check);
        observer.observe(el);
        for (const child of el.children) {
            observer.observe(child);
        }
        return () => {
            el.removeEventListener('scroll', check);
            observer.disconnect();
        };
    }, [active]);

    return [ref, moreBelow] as const;
}
