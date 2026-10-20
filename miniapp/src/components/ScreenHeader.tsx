import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { Flex, IconButton, Typography } from '@maxhub/max-ui';
import { IconChevronLeft } from './Icons';
import { cx } from '../utils/cx';
import s from './ScreenHeader.module.css';

export interface ScreenHeaderProps {
    title: ReactNode;
    /** A line under the title: the set, its size, its author. */
    subtitle?: ReactNode;
    onBack: () => void;
}

/** The back arrow and the title. The arrow stays on the first line of a
    title that wraps, instead of drifting down to the middle of the block.
    A long title stops at three lines, so it does not push the screen into
    a scroll; a tap on it shows the rest. */
export function ScreenHeader({ title, subtitle, onBack }: ScreenHeaderProps) {
    const titleRef = useRef<HTMLHeadingElement>(null);
    const [clamped, setClamped] = useState(false);
    const [expanded, setExpanded] = useState(false);

    // Whether the title is cut is only known once it is laid out,
    // and it changes with the width of the screen.
    useLayoutEffect(() => {
        const el = titleRef.current;
        if (!el || expanded) {
            return;
        }
        const check = () => setClamped(el.scrollHeight > el.clientHeight + 1);
        check();
        const observer = new ResizeObserver(check);
        observer.observe(el);
        return () => observer.disconnect();
    }, [title, expanded]);

    const toggle = clamped || expanded ? () => setExpanded((current) => !current) : undefined;

    return (
        <Flex align="flex-start" gap={8} className={s.header}>
            <IconButton size="small" variant="ghost" aria-label="Назад" className={s.back} onClick={onBack}>
                <IconChevronLeft size={20} />
            </IconButton>
            <Flex direction="column" align="stretch" gap={2} className={s.text}>
                <Typography.Text variant="subheader" asChild>
                    <h1
                        ref={titleRef}
                        className={cx(s.title, !expanded && s.clamped, toggle && s.tappable)}
                        aria-expanded={toggle ? expanded : undefined}
                        onClick={toggle}
                    >
                        {title}
                    </h1>
                </Typography.Text>
                {subtitle ? (
                    <Typography.Text variant="description" color="secondary" className={s.subtitle}>
                        {subtitle}
                    </Typography.Text>
                ) : null}
            </Flex>
        </Flex>
    );
}
