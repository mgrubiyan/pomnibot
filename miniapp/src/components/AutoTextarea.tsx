import { useCallback, useEffect, useLayoutEffect, useRef } from 'react';
import { Textarea, type TextareaProps } from '@maxhub/max-ui';

/**
 * A MAX UI Textarea that grows with its text, so a long question or
 * explanation is read whole instead of scrolled inside a small box.
 * `rows` stays the least height.
 */
export function AutoTextarea(props: TextareaProps) {
    const ref = useRef<HTMLTextAreaElement>(null);

    const fit = useCallback(() => {
        const el = ref.current;
        // A hidden field has no height to measure; it is fitted when shown.
        if (!el || el.offsetParent === null) {
            return;
        }
        el.style.height = 'auto';
        el.style.height = `${el.scrollHeight}px`;
    }, []);

    useLayoutEffect(fit, [fit, props.value]);

    useEffect(() => {
        window.addEventListener('resize', fit);
        return () => window.removeEventListener('resize', fit);
    }, [fit]);

    return <Textarea ref={ref} {...props} />;
}
