import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { Button, Flex, Input, Typography } from '@maxhub/max-ui';
import { api } from '../api';
import type { components } from '../../contracts/schema';
import { telegramApp } from '../platform';
import { Screen } from './Screen';
import { StatusScreen } from './StatusScreen';
import s from './ProfileGate.module.css';

type Profile = components['schemas']['ProfileResponse'];

/** The app mounts learning screens only after the server has resolved the profile. */
export function ProfileGate({ children }: { children: ReactNode }) {
    const [profile, setProfile] = useState<Profile>();
    const [loadError, setLoadError] = useState('');
    const [saveError, setSaveError] = useState('');
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [firstName, setFirstName] = useState('');
    const [lastName, setLastName] = useState('');
    const requestId = useRef(0);

    const load = useCallback(async () => {
        const id = ++requestId.current;
        try {
            const { data, error } = await api.GET('/profile');
            if (requestId.current !== id) return;
            if (error || !data) throw new Error('profile request failed');
            setLoadError('');
            setProfile(data);
            setFirstName(data.user.firstName);
            setLastName(data.user.lastName ?? '');
        } catch {
            if (requestId.current === id) setLoadError('Не удалось загрузить профиль. Повторите запрос.');
        } finally {
            if (requestId.current === id) setLoading(false);
        }
    }, []);

    useEffect(() => {
        void Promise.resolve().then(load);
        const onVisible = () => { if (document.visibilityState === 'visible') void load(); };
        const app = telegramApp();
        document.addEventListener('visibilitychange', onVisible);
        app?.onEvent('activated', onVisible);
        return () => {
            document.removeEventListener('visibilitychange', onVisible);
            app?.offEvent('activated', onVisible);
        };
    }, [load]);

    const reload = () => { setLoading(true); setLoadError(''); void load(); };

    const save = async () => {
        if (saving) return;
        const id = ++requestId.current;
        setSaving(true);
        setSaveError('');
        try {
            const { data, error } = await api.PUT('/profile', { body: { firstName: firstName.trim(), lastName: lastName.trim() } });
            if (requestId.current !== id) return;
            if (error || !data) throw new Error('profile save failed');
            setProfile(data);
        } catch {
            if (requestId.current === id) setSaveError('Не удалось сохранить профиль. Проверьте данные и повторите запрос.');
        } finally {
            setSaving(false);
        }
    };

    if (loading) return <StatusScreen icon={null} title="Загружаем профиль" text="Одну секунду…" />;
    if (!profile || loadError) {
        return <StatusScreen icon={null} title="Профиль недоступен" text={loadError} action={<Button onClick={reload}>Повторить</Button>} />;
    }
    if (profile.platform !== 'telegram' || profile.profileComplete) return children;

    const validName = (value: string) => {
        const name = value.trim();
        return name.length > 0 && !name.startsWith('/') && Array.from(name).length <= 100;
    };
    return (
        <Screen>
            <form className={s.form} onSubmit={event => { event.preventDefault(); void save(); }}>
                <Typography.Title>Как вас зовут?</Typography.Title>
                <Typography.Text>Фамилия и имя будут видны в общем рейтинге наборов.</Typography.Text>
                <Flex direction="column" align="stretch" gap={8}>
                    <label htmlFor="profile-last-name">Фамилия</label>
                    <Input id="profile-last-name" autoComplete="family-name" value={lastName} disabled={saving} onChange={event => setLastName(event.target.value)} />
                    <label htmlFor="profile-first-name">Имя</label>
                    <Input id="profile-first-name" autoComplete="given-name" value={firstName} disabled={saving} onChange={event => setFirstName(event.target.value)} />
                </Flex>
                {saveError && <div role="alert">{saveError}</div>}
                <Button type="submit" stretched disabled={saving || !validName(firstName) || !validName(lastName)}>
                    {saving ? 'Сохраняем…' : saveError ? 'Повторить сохранение' : 'Сохранить'}
                </Button>
                <Button type="button" variant="secondary" disabled={saving} onClick={reload}>Я заполнил профиль в боте</Button>
            </form>
        </Screen>
    );
}
