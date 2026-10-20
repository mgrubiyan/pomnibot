import type { ComponentProps } from 'react';
import { Panel } from '@maxhub/max-ui';
import { cx } from '../utils/cx';
import s from './Screen.module.css';

export function Screen({ className, children, ...rest }: ComponentProps<'div'>) {
    return (
        <Panel mode="secondary" className={cx(s.screen, className)} {...rest}>
            {children}
        </Panel>
    );
}
