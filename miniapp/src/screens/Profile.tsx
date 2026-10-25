import { useId, useRef, useState, type ChangeEvent } from 'react';
import { Button, CellHeader, CellList, CellSimple, Flex, Switch, Typography } from '@maxhub/max-ui';
import { api } from '../api';
import type { Profile as ProfileData } from '../types';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { UserAvatar } from '../components/UserAvatar';
import { avatarFromFile } from '../utils/avatar';
import { ANONYMOUS_MEMBER, formatAuthorName } from '../utils/user';
import s from './Profile.module.css';

/** The full consent; the switch on the screen gives or withdraws it. */
const CONSENT_TEXT = [
    'Я даю команде Помнибота согласие на обработку моих персональных данных: имени, фамилии, имени пользователя, фотографии профиля и результатов повторения — прогресса, ответов и ошибок в наборах карточек.',
    'Цель — показывать эти данные авторам наборов, в которых я участвую, в таблице лидеров и в списке ошибок участников, чтобы автор видел, кому и в чём нужна помощь.',
    'Команда Помнибота хранит эти данные и показывает их только авторам моих наборов. Другим людям и организациям они не передаются.',
    `Согласие действует, пока я его не отзову. Отозвать его можно в любой момент этим же переключателем в профиле — после этого авторы видят меня как «${ANONYMOUS_MEMBER}».`,
];

/** Outside MAX and Telegram the server does not know who is asking. */
const SIGNED_OUT = 'Профиль можно изменить только в приложении в MAX или Telegram.';

export interface ProfileProps {
    profile: ProfileData;
    onChange: (profile: ProfileData) => void;
    onBack: () => void;
}

export function Profile({ profile, onChange, onBack }: ProfileProps) {
    const [saving, setSaving] = useState(false);
    const [failed, setFailed] = useState<false | 'network' | 'signed-out'>(false);
    const [showText, setShowText] = useState(false);
    const textId = useId();
    // The switch moves on the tap; the answer of the server confirms it or
    // takes it back.
    const [pending, setPending] = useState<boolean | null>(null);
    const consent = pending ?? profile.dataConsent;
    const fileRef = useRef<HTMLInputElement>(null);
    const [photoBusy, setPhotoBusy] = useState(false);
    const [photoError, setPhotoError] = useState('');
    // The cut square shows at once, while it is still being uploaded.
    const [preview, setPreview] = useState<string | null>(null);
    const shownUser = preview ? { ...profile.user, avatarUrl: preview } : profile.user;
    const hasPhoto = Boolean(profile.user.avatarUrl);

    const pickPhoto = () => fileRef.current?.click();

    const uploadPhoto = async (event: ChangeEvent<HTMLInputElement>) => {
        const file = event.target.files?.[0];
        // The same file picked again must fire the change once more.
        event.target.value = '';
        if (!file || photoBusy) {
            return;
        }
        setPhotoBusy(true);
        setPhotoError('');
        try {
            let image: string;
            try {
                image = await avatarFromFile(file);
            } catch {
                setPhotoError('Не получилось открыть это фото. Выберите другое.');
                return;
            }
            setPreview(`data:image/jpeg;base64,${image}`);
            const { data, response } = await api.PUT('/profile/avatar', { body: { image } });
            if (response.status === 400) {
                setPhotoError('Это фото не подходит. Выберите другое.');
                return;
            }
            if (response.status === 403) {
                setPhotoError(SIGNED_OUT);
                return;
            }
            if (!response.ok || !data) {
                throw new Error('photo upload failed');
            }
            // The preview stays until the saved photo can be drawn, so the
            // letter does not flash in between.
            if (data.user.avatarUrl) {
                const saved = new Image();
                saved.src = data.user.avatarUrl;
                await saved.decode().catch(() => undefined);
            }
            onChange(data);
        } catch {
            setPhotoError('Не получилось загрузить фото. Проверьте соединение и попробуйте ещё раз.');
        } finally {
            setPreview(null);
            setPhotoBusy(false);
        }
    };

    const removePhoto = async () => {
        if (photoBusy) {
            return;
        }
        setPhotoBusy(true);
        setPhotoError('');
        try {
            const { data, response } = await api.DELETE('/profile/avatar');
            if (response.status === 403) {
                setPhotoError(SIGNED_OUT);
                return;
            }
            if (!response.ok || !data) {
                throw new Error('photo removal failed');
            }
            onChange(data);
        } catch {
            setPhotoError('Не получилось удалить фото. Проверьте соединение и попробуйте ещё раз.');
        } finally {
            setPhotoBusy(false);
        }
    };

    const toggle = async (given: boolean) => {
        if (saving) {
            return;
        }
        setSaving(true);
        setFailed(false);
        setPending(given);
        try {
            const { data, response } = await api.PUT('/profile/consent', { body: { given } });
            if (response.status === 403) {
                setFailed('signed-out');
                return;
            }
            if (!response.ok || !data) {
                throw new Error('consent request failed');
            }
            onChange(data);
        } catch {
            setFailed('network');
        } finally {
            setPending(null);
            setSaving(false);
        }
    };

    return (
        <Screen>
            <ScreenHeader title="Профиль" onBack={onBack} />

            <Flex direction="column" align="stretch" gap={20} className={s.body}>
                <Flex direction="column" align="center" gap={12} className={s.identity}>
                    <button
                        type="button"
                        className={s.photo}
                        onClick={pickPhoto}
                        disabled={photoBusy}
                        aria-label={hasPhoto ? 'Сменить фото' : 'Добавить фото'}
                    >
                        <UserAvatar user={shownUser} size={88} />
                    </button>
                    <input
                        ref={fileRef}
                        type="file"
                        accept="image/*"
                        className={s.file}
                        tabIndex={-1}
                        aria-hidden="true"
                        onChange={(event) => void uploadPhoto(event)}
                    />
                    <Typography.Title className={s.name}>{formatAuthorName(profile.user)}</Typography.Title>
                    <Flex gap={8} justify="center" wrap="wrap">
                        <Button size="small" variant="secondary" loading={photoBusy} onClick={pickPhoto}>
                            {hasPhoto ? 'Сменить фото' : 'Добавить фото'}
                        </Button>
                        {hasPhoto ? (
                            <Button size="small" variant="ghost" disabled={photoBusy} onClick={() => void removePhoto()}>
                                Удалить
                            </Button>
                        ) : null}
                    </Flex>
                    {photoError ? (
                        <Typography.Text variant="description" className={s.photoError} role="alert">
                            {photoError}
                        </Typography.Text>
                    ) : null}
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
                            {failed === 'signed-out' ? SIGNED_OUT : 'Не получилось сохранить. Проверьте соединение и попробуйте ещё раз.'}
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
