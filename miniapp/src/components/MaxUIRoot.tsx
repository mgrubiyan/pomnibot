import { useEffect, useRef, useState, type ReactNode, type RefObject } from 'react';
import { MaxUI, useAppearance } from '@maxhub/max-ui';
import { telegramApp } from '../platform';

/** The MAX UI root that also paints the page behind the app. */
export function MaxUIRoot({ children }: { children: ReactNode }) {
    const root = useRef<HTMLDivElement>(null);
    const telegram = telegramApp();
    const [theme, setTheme] = useState(telegram?.colorScheme);
    useEffect(() => {
        if (!telegram) return;
        const update = () => {
            setTheme(telegram.colorScheme);
            for (const side of ['top', 'right', 'bottom', 'left'] as const) {
                const inset = (telegram.safeAreaInset?.[side] ?? 0) + (telegram.contentSafeAreaInset?.[side] ?? 0);
                document.documentElement.style.setProperty(`--telegram-safe-${side}`, `${inset}px`);
            }
        };
        update();
        const events = ['themeChanged', 'safeAreaChanged', 'contentSafeAreaChanged'];
        events.forEach(event => telegram.onEvent(event, update));
        return () => events.forEach(event => telegram.offEvent(event, update));
    }, [telegram]);
    return (
        <MaxUI ref={root} resetBody colorScheme={theme}>
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
        // Native controls and scrollbars follow the theme too (MAX web).
        document.documentElement.style.colorScheme = colorScheme;

        // Telegram ignores theme-color and paints its bars itself; it takes #RRGGBB only.
        // Clients older than 6.9 throw on a color value, which must not take the app down.
        const telegram = telegramApp();
        if (telegram && /^#[0-9a-f]{6}$/i.test(color)) {
            try {
                telegram.setHeaderColor?.(color);
                telegram.setBackgroundColor?.(color);
                telegram.setBottomBarColor?.(color);
            } catch {
                // The bars keep Telegram's own color.
            }
        }

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
