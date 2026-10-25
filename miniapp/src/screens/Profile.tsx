import { useId, useState } from 'react';
import { CellHeader, CellList, CellSimple, Flex, Switch, Typography } from '@maxhub/max-ui';
import { api } from '../api';
import type { Profile as ProfileData } from '../types';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { UserAvatar } from '../components/UserAvatar';
import { ANONYMOUS_MEMBER, formatAuthorName } from '../utils/user';
import s from './Profile.module.css';

/** The full consent; the switch on the screen gives or withdraws it. */
const CONSENT_TEXT = [
    'Я даю команде Помнибота согласие на обработку моих персональных данных: имени, фамилии, имени пользователя, фотографии профиля и результатов повторения — прогресса, ответов и ошибок в наборах карточек.',
    'Цель — показывать эти данные авторам наборов, в которых я участвую, в таблице лидеров и в списке ошибок участников, чтобы автор видел, кому и в чём нужна помощь.',
    'Команда Помнибота хранит эти данные и показывает их только авторам моих наборов. Другим людям и организациям они не передаются.',
    `Согласие действует, пока я его не отзову. Отозвать его можно в любой момент этим же переключателем в профиле — после этого авторы видят меня как «${ANONYMOUS_MEMBER}».`,
];

export interface ProfileProps {
    profile: ProfileData;
    onChange: (profile: ProfileData) => void;
    onBack: () => void;
}

export function Profile({ profile, onChange, onBack }: ProfileProps) {
    const [saving, setSaving] = useState(false);
    const [failed, setFailed] = useState(false);
    const [showText, setShowText] = useState(false);
    const textId = useId();
    const consent = profile.dataConsent;

    const toggle = async (given: boolean) => {
        if (saving) {
            return;
        }
        setSaving(true);
        setFailed(false);
        try {
            const { data, response } = await api.PUT('/profile/consent', { body: { given } });
            if (!response.ok || !data) {
                throw new Error('consent request failed');
            }
            onChange(data);
        } catch {
            setFailed(true);
        } finally {
            setSaving(false);
        }
    };

    return (
        <Screen>
            <ScreenHeader title="Профиль" onBack={onBack} />

            <Flex direction="column" align="stretch" gap={20} className={s.body}>
                <Flex direction="column" align="center" gap={12} className={s.identity}>
                    <UserAvatar user={profile.user} size={88} />
                    <Typography.Title className={s.name}>{formatAuthorName(profile.user)}</Typography.Title>
                </Flex>

                <Flex direction="column" align="stretch" gap={8}>
                    <CellHeader titleStyle="normal">Авторы наборов</CellHeader>
                    <CellList mode="island" filled className={s.list}>
                        <CellSimple
                            as="label"
                            title="Показывать моё имя"
                            subtitle="В таблице лидеров и ошибках участников"
                            after={
                                <Switch
                                    checked={consent}
                                    disabled={saving}
                                    aria-describedby={textId}
                                    onChange={(event) => void toggle(event.target.checked)}
                                />
                            }
                        />
                    </CellList>
                    <Typography.Text variant="description" color="secondary" className={s.note}>
                        {consent
                            ? 'Автор видит ваши имя, фото и ошибки — так он поймёт, кому и с чем помочь.'
                            : `Сейчас авторы видят вас как «${ANONYMOUS_MEMBER}»: результаты без имени и фото.`}
                    </Typography.Text>
                    {failed ? (
                        <Typography.Text variant="description" className={s.error} role="alert">
                            Не получилось сохранить. Проверьте соединение и попробуйте ещё раз.
                        </Typography.Text>
                    ) : null}
                </Flex>

                <Flex direction="column" align="stretch" gap={8} id={textId}>
                    <Typography.Text variant="label" color="tertiary" className={s.note}>
                        Включая переключатель, вы соглашаетесь на обработку персональных данных.{' '}
                        <button
                            type="button"
                            className={s.more}
                            aria-expanded={showText}
                            onClick={() => setShowText((current) => !current)}
                        >
                            {showText ? 'Скрыть текст согласия' : 'Текст согласия'}
                        </button>
                    </Typography.Text>
                    {showText ? (
                        <Flex direction="column" align="stretch" gap={8} className={s.consentText}>
                            {CONSENT_TEXT.map((paragraph) => (
                                <Typography.Text key={paragraph} variant="label" color="secondary">
                                    {paragraph}
                                </Typography.Text>
                            ))}
                        </Flex>
                    ) : null}
                </Flex>
            </Flex>
        </Screen>
    );
}

export default Profile;
