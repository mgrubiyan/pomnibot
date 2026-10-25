import { useCallback, useEffect, useLayoutEffect, useRef } from 'react';
import { mergeRefs, Textarea, type TextareaProps } from '@maxhub/max-ui';

/**
 * A MAX UI Textarea that grows with its text, so a long question or
 * explanation is read whole instead of scrolled inside a small box.
 * `rows` stays the least height. A ref from outside is merged with the
 * field's own, which it needs to grow.
 */
export function AutoTextarea({ ref: outerRef, ...props }: TextareaProps) {
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

    return <Textarea {...props} ref={mergeRefs(ref, outerRef)} />;
}
