import { Flex, IconButton, Typography } from '@maxhub/max-ui';
import { IconChevronLeft } from './Icons';
import s from './ScreenHeader.module.css';

export interface ScreenHeaderProps {
    title: string;
    onBack: () => void;
}

/** The back arrow and the title, as the code and cards screens have them. */
export function ScreenHeader({ title, onBack }: ScreenHeaderProps) {
    return (
        <Flex align="center" gap={8} className={s.header}>
            <IconButton size="small" variant="ghost" aria-label="Назад" onClick={onBack}>
                <IconChevronLeft size={20} />
            </IconButton>
            <Typography.Text variant="subheader" asChild>
                <h1 className={s.title}>{title}</h1>
            </Typography.Text>
        </Flex>
    );
}
