import { Avatar, type AvatarTextGradient } from '@maxhub/max-ui';
import type { User } from '../types';
import { avatarLetter } from '../utils/user';
import { IconPerson } from './Icons';

const gradients: AvatarTextGradient[] = ['blue', 'green', 'purple', 'orange', 'red'];

/** The same color for a person every time, from their ID. */
function gradientOf(id: string): AvatarTextGradient {
    let hash = 0;
    for (const char of id) {
        hash = (hash * 31 + char.charCodeAt(0)) | 0;
    }
    return gradients[Math.abs(hash) % gradients.length];
}

export interface UserAvatarProps {
    user: User;
    size: number;
}

/** A person's round avatar: their photo, the first letter of their name,
    or an outline for a member who has not let the author see them. */
export function UserAvatar({ user, size }: UserAvatarProps) {
    const letter = avatarLetter(user);
    const fallback = letter ? (
        <Avatar.Text gradient={gradientOf(user.id)}>{letter}</Avatar.Text>
    ) : (
        <Avatar.Icon>
            <IconPerson size={Math.round(size / 2)} tone="muted" />
        </Avatar.Icon>
    );
    return (
        <Avatar.Container size={size} form="circle">
            {user.avatarUrl ? <Avatar.Image src={user.avatarUrl} alt="" fallback={fallback} /> : fallback}
        </Avatar.Container>
    );
}
