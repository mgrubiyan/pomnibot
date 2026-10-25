import { useEffect, useRef, useState } from 'react';
import { Flex, Typography } from '@maxhub/max-ui';
import { ANONYMOUS_MEMBER } from '../utils/user';
import s from './ConsentText.module.css';

/** The full consent; the first visit and the switch in the profile give or withdraw it. */
const CONSENT_TEXT = [
    'Я даю команде Помнибота согласие на обработку моих персональных данных: имени, фамилии, имени пользователя, фотографии профиля и результатов повторения — прогресса, ответов и ошибок в наборах карточек.',
    'Цель — показывать эти данные авторам наборов, в которых я участвую, в таблице лидеров и в списке ошибок участников, чтобы автор видел, кому и в чём нужна помощь.',
    'Команда Помнибота хранит эти данные и показывает их только авторам моих наборов. Другим людям и организациям они не передаются.',
    `Согласие действует, пока я его не отзову. Отозвать его можно в любой момент переключателем в профиле — после этого авторы видят меня как «${ANONYMOUS_MEMBER}».`,
];

export interface ConsentTextProps {
    /** Ties the text to the control that gives the consent. */
    id?: string;
    /** What giving the consent takes, e.g. «Включая переключатель». */
    lead: string;
}

/** One line on what the control means, and the full text under a tap. */
export function ConsentText({ id, lead }: ConsentTextProps) {
    const [open, setOpen] = useState(false);
    const textRef = useRef<HTMLDivElement>(null);

    // The text opens under the fold on a short phone; bring it up so the
    // tap visibly did something.
    useEffect(() => {
        if (!open) return;
        const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
        textRef.current?.scrollIntoView({ block: 'nearest', behavior: reduce ? 'auto' : 'smooth' });
    }, [open]);

    return (
        <Flex direction="column" align="stretch" gap={8} id={id}>
            <Typography.Text variant="label" color="tertiary" className={s.lead}>
                {lead}, вы соглашаетесь на обработку персональных данных.{' '}
                <button type="button" className={s.more} aria-expanded={open} onClick={() => setOpen((current) => !current)}>
                    {open ? 'Скрыть текст согласия' : 'Текст согласия'}
                </button>
            </Typography.Text>
            {open ? (
                <Flex ref={textRef} direction="column" align="stretch" gap={8} className={s.text}>
                    {CONSENT_TEXT.map((paragraph) => (
                        <Typography.Text key={paragraph} variant="label" color="secondary">
                            {paragraph}
                        </Typography.Text>
                    ))}
                </Flex>
            ) : null}
        </Flex>
    );
}
