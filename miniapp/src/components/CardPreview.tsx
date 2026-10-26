import { Flex, Typography } from '@maxhub/max-ui';
import type { Card, CardAnswer } from '../types';
import { StudyContent } from './StudyContent';
import { StudyImages } from './StudyImages';
import { StudyIllustration } from './StudyIllustration';
import s from './CardPreview.module.css';

function Answer({ answer, options }: { answer?: CardAnswer; options?: string[] }) {
    if (answer === undefined) return null;
    if (Array.isArray(answer)) {
        return <ul className={s.answers}>{answer.map((text, index) => <li key={index}><StudyContent text={text} /></li>)}</ul>;
    }
    if (typeof answer === 'object') {
        return (
            <div className={s.columns}>
                {answer.columns.map((column) => (
                    <div key={column} className={s.column}>
                        <Typography.Text variant="body-strong" asChild><StudyContent text={column} /></Typography.Text>
                        <ul className={s.answers}>
                            {answer.items.filter((item) => item.column === column).map((item, index) => (
                                <li key={index}><StudyContent text={item.text} /></li>
                            ))}
                        </ul>
                    </div>
                ))}
            </div>
        );
    }
    const text = typeof answer === 'boolean'
        ? answer ? 'Верно' : 'Неверно'
        : typeof answer === 'number' ? options?.[answer] ?? String(answer) : answer;
    return <StudyContent text={text} />;
}

/** The author can read the rendered card while editing its source text. */
export function CardPreview({ card }: { card: Partial<Card> }) {
    return (
        <Flex direction="column" align="stretch" gap={16} className={s.preview}>
            <Flex direction="column" align="stretch" gap={8}>
                <Typography.Text variant="label" color="tertiary">Вопрос</Typography.Text>
                <Typography.Text variant="body-strong" asChild><StudyContent text={card.question ?? ''} /></Typography.Text>
                <StudyImages images={card.questionImages} />
                <StudyIllustration svg={card.questionSvg} alt="Иллюстрация к вопросу" />
            </Flex>
            {card.kind === 'choice' && card.options?.length ? (
                <Flex direction="column" align="stretch" gap={4}>
                    <Typography.Text variant="label" color="tertiary">Варианты ответа</Typography.Text>
                    <ul className={s.answers}>
                        {card.options.map((text, index) => <li key={index}><StudyContent text={text} /></li>)}
                    </ul>
                </Flex>
            ) : null}
            <Flex direction="column" align="stretch" gap={4}>
                <Typography.Text variant="label" color="tertiary">Верный ответ</Typography.Text>
                <Answer answer={card.kind === 'table' ? card.table : card.answer} options={card.options} />
            </Flex>
            {card.explanation || card.explanationSvg || card.explanationImages?.length ? (
                <Flex direction="column" align="stretch" gap={8}>
                    <Typography.Text variant="label" color="tertiary">Объяснение</Typography.Text>
                    <StudyContent text={card.explanation ?? ''} />
                    <StudyImages images={card.explanationImages} />
                    <StudyIllustration svg={card.explanationSvg} alt="Иллюстрация к объяснению" />
                </Flex>
            ) : null}
        </Flex>
    );
}
