import type { ReactNode } from 'react';
import { Flex, Typography } from '@maxhub/max-ui';
import { Screen } from './Screen';
import s from './StatusScreen.module.css';

export interface StatusScreenProps {
    icon: ReactNode;
    title: string;
    text: string;
    action?: ReactNode;
}

/** Shared frame for loading, error, end of feed and empty states. */
export function StatusScreen({ icon, title, text, action }: StatusScreenProps) {
    return (
        <Screen>
            <Flex direction="column" align="center" justify="center" gap={12} className={s.status}>
                {icon}
                <Typography.Text variant="subheader">{title}</Typography.Text>
                <Typography.Text variant="body" color="secondary" className={s.statusText}>
                    {text}
                </Typography.Text>
            </Flex>
            {action ? (
                <Flex direction="column" align="stretch">
                    {action}
                </Flex>
            ) : null}
        </Screen>
    );
}
