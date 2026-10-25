import { useState, type ReactNode } from 'react';
import { Button, Flex, Typography } from '@maxhub/max-ui';
import { api } from '../api';
import { config } from '../config';
import { ANONYMOUS_MEMBER, PROFILE_SIGNED_OUT } from '../utils/user';
import { ConsentText } from './ConsentText';
import { IconPerson } from './Icons';
import { useProfile } from './profileContext';
import { Screen } from './Screen';
import { cx } from '../utils/cx';
import { useMoreBelow } from '../utils/useMoreBelow';
import s from './ConsentGate.module.css';

/** On the first visit the user says whether set authors may see them; the
    answer can be changed in the profile at any time. */
export function ConsentGate({ children }: { children: ReactNode }) {
    const state = useProfile();
    const [saving, setSaving] = useState<boolean | null>(null);
    const [failed, setFailed] = useState<false | 'network' | 'signed-out'>(false);
    // After a failed save the user may go on and be asked on the next visit.
    const [later, setLater] = useState(false);
    const [bodyRef, moreBelow] = useMoreBelow<HTMLDivElement>(true);

    // Without a signature nobody can answer, except locally, where the
    // browser account stands for the developer.
    const canAnswer = state && (state.profile.platform || config.environment === 'local');
    if (!state || !canAnswer || state.profile.dataConsentAnswered || later) {
        return children;
    }

    const answer = async (given: boolean) => {
        if (saving !== null) return;
        setSaving(given);
        setFailed(false);
        try {
            const { data, response } = await api.PUT('/profile/consent', { body: { given } });
            if (response.status === 403) {
                setFailed('signed-out');
                return;
            }
            if (!response.ok || !data) throw new Error('consent request failed');
            state.setProfile(data);
        } catch {
            setFailed('network');
        } finally {
            setSaving(null);
        }
    };

    return (
        <Screen>
            {/* The question and the opened consent scroll, the buttons stay put. */}
            <Flex ref={bodyRef} direction="column" align="stretch" gap={12} className={cx(s.body, moreBelow && s.moreBelow)}>
                <Flex direction="column" align="center" gap={12} className={s.question}>
                    <IconPerson size={48} tone="muted" />
                    <Typography.Text variant="subheader" className={s.title}>
                        Показывать вас авторам наборов?
                    </Typography.Text>
                    <Typography.Text variant="body" color="secondary" className={s.text}>
                        Автор увидит ваши имя, фото и ошибки в таблице лидеров и списке ошибок — так он поймёт, кому и с чем помочь.
                        Без разрешения вы для него «{ANONYMOUS_MEMBER}».
                    </Typography.Text>
                    <Typography.Text variant="body" color="secondary" className={s.text}>
                        Это можно изменить в любой момент в профиле.
                    </Typography.Text>
                </Flex>
                <ConsentText lead="Нажимая «Разрешить»" />
            </Flex>

            <Flex direction="column" align="stretch" gap={12}>
                {failed ? (
                    <Typography.Text variant="description" className={s.error} role="alert">
                        {failed === 'signed-out' ? PROFILE_SIGNED_OUT : 'Не получилось сохранить. Проверьте соединение и попробуйте ещё раз.'}
                    </Typography.Text>
                ) : null}
                <Flex direction="column" align="stretch" gap={8}>
                    <Button size="medium" variant="primary" stretched disabled={saving !== null} onClick={() => void answer(true)}>
                        {saving === true ? 'Сохраняем…' : 'Разрешить'}
                    </Button>
                    <Button size="medium" variant="secondary" stretched disabled={saving !== null} onClick={() => void answer(false)}>
                        {saving === false ? 'Сохраняем…' : 'Не показывать'}
                    </Button>
                    {failed ? (
                        <Button size="medium" variant="ghost" stretched disabled={saving !== null} onClick={() => setLater(true)}>
                            Спросить потом
                        </Button>
                    ) : null}
                </Flex>
            </Flex>
        </Screen>
    );
}
