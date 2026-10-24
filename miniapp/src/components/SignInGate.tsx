import { useSyncExternalStore, type ReactNode } from 'react';
import { Button } from '@maxhub/max-ui';
import { closeMiniApp } from '../platform';
import { onSignInExpired, signInExpired } from '../utils/signIn';
import { IconAlert } from './Icons';
import { StatusScreen } from './StatusScreen';

/**
 * Once the backend refuses the sign-in, every screen would fail, so the app
 * asks the reader to reopen it instead.
 */
export function SignInGate({ children }: { children: ReactNode }) {
    const expired = useSyncExternalStore(onSignInExpired, signInExpired);
    if (!expired) {
        return children;
    }
    const close = closeMiniApp();
    return (
        <StatusScreen
            icon={<IconAlert size={48} tone="muted" />}
            title="Вход устарел"
            text="Закройте приложение и откройте его снова"
            action={
                close ? (
                    <Button size="medium" variant="primary" stretched onClick={close}>
                        Закрыть
                    </Button>
                ) : undefined
            }
        />
    );
}
