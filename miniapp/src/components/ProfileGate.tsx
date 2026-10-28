import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Button } from '@maxhub/max-ui';
import { api } from '../api';
import type { components } from '../../contracts/schema';
import { ProfileContext } from './profileContext';
import { StatusScreen } from './StatusScreen';

type Profile = components['schemas']['ProfileResponse'];

/** The app mounts learning screens only after the server has resolved the profile. */
export function ProfileGate({ children }: { children: ReactNode }) {
    const [profile, setProfile] = useState<Profile>();
    const [loadError, setLoadError] = useState('');
    const [loading, setLoading] = useState(true);
    const requestId = useRef(0);

    const load = useCallback(async () => {
        const id = ++requestId.current;
        try {
            const { data, error } = await api.GET('/profile');
            if (requestId.current !== id) return;
            if (error || !data) throw new Error('profile request failed');
            setLoadError('');
            setProfile(data);
        } catch {
            if (requestId.current === id) setLoadError('Не удалось загрузить профиль. Повторите запрос.');
        } finally {
            if (requestId.current === id) setLoading(false);
        }
    }, []);

    useEffect(() => {
        void Promise.resolve().then(load);
        const onVisible = () => { if (document.visibilityState === 'visible') void load(); };
        document.addEventListener('visibilitychange', onVisible);
        return () => document.removeEventListener('visibilitychange', onVisible);
    }, [load]);

    // A screen that saved the profile has the newest one: a reload sent
    // before the save must not bring the old one back.
    const shared = useMemo(() => profile ? {
        profile,
        setProfile: (next: Profile) => {
            requestId.current++;
            setProfile(next);
        },
    } : null, [profile]);

    const reload = () => { setLoading(true); setLoadError(''); void load(); };

    if (loading) return <StatusScreen icon={null} title="Загружаем профиль" text="Одну секунду…" />;
    // A reload that fails in the background, as on the way back from the
    // photo picker, keeps the profile already on screen.
    if (!profile) {
        return <StatusScreen icon={null} title="Профиль недоступен" text={loadError} action={<Button onClick={reload}>Повторить</Button>} />;
    }
    return <ProfileContext.Provider value={shared}>{children}</ProfileContext.Provider>;
}
