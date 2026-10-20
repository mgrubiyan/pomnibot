import { useEffect, useRef, type ReactNode, type RefObject } from 'react';
import { MaxUI, useAppearance } from '@maxhub/max-ui';

/** The MAX UI root that also paints the page behind the app. */
export function MaxUIRoot({ children }: { children: ReactNode }) {
    const root = useRef<HTMLDivElement>(null);
    return (
        <MaxUI ref={root} resetBody>
            <PageBackground root={root} />
            {children}
        </MaxUI>
    );
}

/**
 * Paints the page behind the app in the screen color. What the browser shows
 * past the app — overscroll, safe areas, its own bars — is otherwise white,
 * glaring in the dark theme. MAX UI keeps its colors on its root element, so
 * the color is read from there and follows the color scheme.
 */
function PageBackground({ root }: { root: RefObject<HTMLElement | null> }) {
    const { colorScheme } = useAppearance();

    useEffect(() => {
        if (!root.current) return;
        const color = getComputedStyle(root.current)
            .getPropertyValue('--background-surface')
            .trim();
        if (!color) return;

        document.documentElement.style.backgroundColor = color;

        let meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
        if (!meta) {
            meta = document.createElement('meta');
            meta.name = 'theme-color';
            document.head.append(meta);
        }
        meta.content = color;
    }, [root, colorScheme]);

    return null;
}
