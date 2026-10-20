import { useCallback, useEffect, useLayoutEffect, useRef } from 'react';
import { Textarea, type TextareaProps } from '@maxhub/max-ui';

/**
 * A MAX UI Textarea that grows with its text, so a long question or
 * explanation is read whole instead of scrolled inside a small box.
 * `rows` stays the least height. The field keeps its ref to itself: a ref
 * from outside would take its place and the field would stop growing.
 */
export function AutoTextarea(props: Omit<TextareaProps, 'ref'>) {
    const ref = useRef<HTMLTextAreaElement>(null);

    const fit = useCallback(() => {
        const el = ref.current;
        // A hidden field has no height to measure.
        if (!el || el.offsetParent === null) {
            return;
        }
        el.style.height = 'auto';
        el.style.height = `${el.scrollHeight}px`;
    }, []);

    useLayoutEffect(fit, [fit, props.value]);

    // Refit when the width changes, and when a hidden form is shown again:
    // both change the width the observer sees. A change of height alone is
    // the field's own fitting and is let pass.
    useEffect(() => {
        const el = ref.current;
        if (!el) {
            return;
        }
        let width = -1;
        const observer = new ResizeObserver(([entry]) => {
            if (entry.contentRect.width !== width) {
                width = entry.contentRect.width;
                fit();
            }
        });
        observer.observe(el);
        return () => observer.disconnect();
    }, [fit]);

    return <Textarea {...props} ref={ref} />;
}
