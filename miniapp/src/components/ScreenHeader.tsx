import type { ReactNode } from 'react';
import { Flex, IconButton, Typography } from '@maxhub/max-ui';
import { IconChevronLeft } from './Icons';
import s from './ScreenHeader.module.css';

export interface ScreenHeaderProps {
    title: ReactNode;
    /** A line under the title: the set, its size, its author. */
    subtitle?: ReactNode;
    onBack: () => void;
}

/** The back arrow and the title. The arrow stays on the first line of a
    title that wraps, instead of drifting down to the middle of the block. */
export function ScreenHeader({ title, subtitle, onBack }: ScreenHeaderProps) {
    return (
        <Flex align="flex-start" gap={8} className={s.header}>
            <IconButton size="small" variant="ghost" aria-label="Назад" className={s.back} onClick={onBack}>
                <IconChevronLeft size={20} />
            </IconButton>
            <Flex direction="column" align="stretch" gap={2} className={s.text}>
                <Typography.Text variant="subheader" asChild>
                    <h1 className={s.title}>{title}</h1>
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
