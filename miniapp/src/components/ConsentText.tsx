import { useEffect, useRef, useState } from 'react';
import { Flex, Typography } from '@maxhub/max-ui';
import { motionAllowed } from '../utils/motion';
import s from './ConsentText.module.css';

/** The full consent; joining a set gives it, leaving the set withdraws it. */
const CONSENT_TEXT = [
    'Я даю команде Помнибота согласие на обработку моих персональных данных: имени, фамилии, имени пользователя, фотографии профиля и результатов повторения — прогресса, ответов и ошибок в наборах карточек.',
    'Цель — показывать эти данные авторам наборов, в которых я участвую, в таблице лидеров и в списке ошибок участников, чтобы автор видел, кому и в чём нужна помощь.',
    'Команда Помнибота хранит эти данные и показывает их только авторам моих наборов. Другим людям и организациям они не передаются.',
    'Согласие даётся автору набора, к которому я присоединяюсь, и действует, пока я в этом наборе. Отозвать его можно в любой момент, выйдя из набора, — после этого автор больше не видит меня и мои результаты.',
];

export interface ConsentTextProps {
    /** What giving the consent takes, e.g. «Нажимая «Добавить набор»». */
    lead: string;
}

/** One line on what the control means, and the full text under a tap. */
export function ConsentText({ lead }: ConsentTextProps) {
    const [open, setOpen] = useState(false);
    const rootRef = useRef<HTMLDivElement>(null);

    // The text opens under the fold on a short phone; bring it up so the
    // tap visibly did something. The whole block is the target: the link
    // just tapped stays on top when the text is taller than the screen,
    // and the text's own rise-in transform does not skew the measure.
    useEffect(() => {
        if (!open) return;
        rootRef.current?.scrollIntoView({ block: 'nearest', behavior: motionAllowed() ? 'smooth' : 'auto' });
    }, [open]);

    return (
        <Flex ref={rootRef} direction="column" align="stretch" gap={8}>
            <Typography.Text variant="label" color="tertiary" className={s.lead}>
                {lead}, вы соглашаетесь на обработку персональных данных.{' '}
                <button type="button" className={s.more} aria-expanded={open} onClick={() => setOpen((current) => !current)}>
                    {open ? 'Скрыть текст согласия' : 'Текст согласия'}
                </button>
            </Typography.Text>
            {open ? (
                <Flex direction="column" align="stretch" gap={8} className={s.text}>
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
