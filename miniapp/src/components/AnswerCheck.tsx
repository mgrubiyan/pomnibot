import { Button, Flex, Spinner, Typography } from '@maxhub/max-ui';
import { IconDoc, IconOffline } from './Icons';
import s from './AnswerCheck.module.css';

/**
 * checking — the backend is judging the answer; slow — it takes longer than
 * usual; failed — it did not answer at all; shown — the reader stopped waiting.
 */
export type CheckStage = 'checking' | 'slow' | 'failed' | 'shown';

export interface AnswerCheckProps {
    stage: CheckStage;
    answer: string;
    sourceQuote: string;
    sourceRef?: string;
    /** Nobody judged the answer, so the reader does it. */
    onSelfCheck: (correct: boolean) => void;
}

/** The face of a card whose typed answer is being checked. */
export function AnswerCheck({ stage, answer, sourceQuote, sourceRef, onSelfCheck }: AnswerCheckProps) {
    const waiting = stage === 'checking' || stage === 'slow';

    return (
        <>
            <Flex direction="column" align="stretch" gap={4}>
                <Typography.Text variant="label" color="tertiary">
                    Ваш ответ
                </Typography.Text>
                <Typography.Text variant="body-strong" asChild>
                    <p className={s.answer}>{answer}</p>
                </Typography.Text>
            </Flex>

            <Flex direction="column" align="stretch" gap={12} aria-live="polite">
                <div className={s.divider} />

                {waiting ? (
                    <Flex direction="column" align="stretch" gap={4}>
                        <Flex align="center" gap={8}>
                            <Spinner size={16} appearance="neutral-themed" />
                            <Typography.Text variant="body" color="secondary">
                                Сверяю с конспектом
                            </Typography.Text>
                        </Flex>
                        {stage === 'slow' ? (
                            <Typography.Text variant="description" color="tertiary">
                                Это занимает больше обычного
                            </Typography.Text>
                        ) : null}
                    </Flex>
                ) : (
                    <>
                        {stage === 'failed' ? (
                            <Flex align="center" gap={8}>
                                <IconOffline size={20} tone="muted" />
                                <Typography.Text variant="body" color="secondary">
                                    Не удалось проверить автоматически
                                </Typography.Text>
                            </Flex>
                        ) : null}

                        <figure className={s.source}>
                            <Flex direction="column" align="stretch" gap={4}>
                                <Typography.Text variant="label-strong" color="tertiary" asChild>
                                    <figcaption>
                                        <Flex align="center" gap={4}>
                                            <IconDoc size={14} tone="muted" />
                                            Эталон из вашего конспекта
                                            {sourceRef ? ` · ${sourceRef}` : ''}
                                        </Flex>
                                    </figcaption>
                                </Typography.Text>
                                <Typography.Text variant="description" color="tertiary" asChild>
                                    <blockquote className={s.quote}>«{sourceQuote}»</blockquote>
                                </Typography.Text>
                            </Flex>
                        </figure>

                        <Flex direction="column" align="stretch" gap={8}>
                            <Typography.Text variant="label" color="tertiary">
                                Сравните сами
                            </Typography.Text>
                            <Flex gap={8} align="stretch">
                                <Button
                                    size="medium"
                                    variant="secondary"
                                    stretched
                                    onClick={() => onSelfCheck(true)}
                                >
                                    Я был прав
                                </Button>
                                <Button
                                    size="medium"
                                    variant="secondary"
                                    stretched
                                    onClick={() => onSelfCheck(false)}
                                >
                                    Ошибся
                                </Button>
                            </Flex>
                        </Flex>
                    </>
                )}
            </Flex>
        </>
    );
}

export default AnswerCheck;
