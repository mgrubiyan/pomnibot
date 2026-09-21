import type { ReactNode } from 'react';
import { cx } from '../utils/cx';
import s from './Icons.module.css';

/** Semantic icon color; without a tone the icon inherits the text color. */
export type IconTone = 'positive' | 'negative' | 'muted';

export interface IconProps {
    size?: number;
    tone?: IconTone;
    className?: string;
}

const toneClass: Record<IconTone, string> = {
    positive: s.iconPositive,
    negative: s.iconNegative,
    muted: s.iconMuted,
};

function Icon({ size = 24, tone, className, children }: IconProps & { children: ReactNode }) {
    return (
        <svg
            className={cx(s.icon, tone && toneClass[tone], className)}
            width={size}
            height={size}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={2}
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
        >
            {children}
        </svg>
    );
}

export const IconCheck = (props: IconProps) => (
    <Icon {...props}>
        <circle cx="12" cy="12" r="10" />
        <path d="M7.5 12.5l3 3 6-6.5" />
    </Icon>
);

export const IconCross = (props: IconProps) => (
    <Icon {...props}>
        <circle cx="12" cy="12" r="10" />
        <path d="M9 9l6 6M15 9l-6 6" />
    </Icon>
);

export const IconMic = (props: IconProps) => (
    <Icon {...props}>
        <rect x="9" y="3" width="6" height="11" rx="3" />
        <path d="M5 11a7 7 0 0 0 14 0M12 18v3" />
    </Icon>
);

export const IconStop = (props: IconProps) => (
    <Icon {...props}>
        <rect x="7" y="7" width="10" height="10" rx="2" />
    </Icon>
);

export const IconDoc = (props: IconProps) => (
    <Icon {...props}>
        <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
        <path d="M14 3v5h5M9 13h6M9 17h4" />
    </Icon>
);

export const IconFlag = (props: IconProps) => (
    <Icon {...props}>
        <path d="M5 21V4M5 4h11l-2 4 2 4H5" />
    </Icon>
);

export const IconOffline = (props: IconProps) => (
    <Icon {...props}>
        <path d="M3 3l18 18" />
        <path d="M8.5 8.6A5 5 0 0 0 6 18h11" />
        <path d="M20.5 16.3A4 4 0 0 0 17 10h-.6A6 6 0 0 0 10.4 6.2" />
    </Icon>
);

export const IconChevronLeft = (props: IconProps) => (
    <Icon {...props}>
        <path d="M15 6l-6 6 6 6" />
    </Icon>
);

export const IconTrash = (props: IconProps) => (
    <Icon {...props}>
        <path d="M4 7h16M10 11v6M14 11v6" />
        <path d="M6 7l1 13a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-13M9 7V4h6v3" />
    </Icon>
);

export const IconPlus = (props: IconProps) => (
    <Icon {...props}>
        <path d="M12 5v14M5 12h14" />
    </Icon>
);
