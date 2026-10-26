import { useState, type ReactNode } from 'react';
import { Flex, Typography } from '@maxhub/max-ui';
import { cx } from '../utils/cx';
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
    // A screen that moves through steps, as the notes' upload does, fades
    // each new line in; the first lines show at once, like the rest of it.
    const [first] = useState({ title, text });
    return (
        <Screen>
            <Flex direction="column" align="center" justify="center" gap={12} className={s.status}>
                {icon}
                <Typography.Text
                    key={`title:${title}`}
                    variant="subheader"
                    className={cx(s.statusTitle, title !== first.title && s.swap)}
                >
                    {title}
                </Typography.Text>
                <Typography.Text
                    key={`text:${text}`}
                    variant="body"
                    color="secondary"
                    className={cx(s.statusText, text !== first.text && s.swap)}
                >
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
