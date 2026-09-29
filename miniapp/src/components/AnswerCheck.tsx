import { Flex, Spinner, Typography } from '@maxhub/max-ui';
import s from './AnswerCheck.module.css';

/** How long the reader has waited: the words change, the wait never ends early. */
export type CheckStage = 'checking' | 'longer' | 'almost';

const MESSAGE: Record<CheckStage, string> = {
    checking: 'Сверяю с конспектом',
    longer: 'Подождите ещё',
    almost: 'Ещё чуть-чуть',
};

export interface AnswerCheckProps {
    stage: CheckStage;
    answer: string;
}

/** The face of a card whose typed answer is being checked. */
export function AnswerCheck({ stage, answer }: AnswerCheckProps) {
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

            <Flex direction="column" align="stretch" gap={12}>
                <div className={s.divider} />
                <Flex align="center" gap={8} aria-live="polite">
                    <Spinner size={16} appearance="neutral-themed" />
                    <Typography.Text variant="body" color="secondary">
                        {MESSAGE[stage]}
                    </Typography.Text>
                </Flex>
            </Flex>
        </>
    );
}

export default AnswerCheck;
