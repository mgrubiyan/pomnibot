import { Button, Flex, Typography } from '@maxhub/max-ui';
import type { Answer, Card } from '../types';
import { Screen } from '../components/Screen';
import { IconCalendar, IconCheck } from '../components/Icons';
import { errorsLabel } from '../utils/plural';
import { allCorrect, factsWithErrors } from '../utils/results';
import s from './Result.module.css';

export interface ResultProps {
    /** The set that was gone through; none for the daily mix from Home. */
    setTitle?: string;
    cards: Card[];
    results: Answer[];
    onExit: () => void;
    /** Only a single set can be shared; the daily mix has nothing to share. */
    onShare?: () => void;
}

/** End of a run: the score and the facts that went worst. */
export function Result({ setTitle, cards, results, onExit, onShare }: ResultProps) {
    const correct = results.filter((result) => result.correct).length;
    const facts = factsWithErrors(cards, results);
    const allRight = allCorrect(results);

    return (
        <Screen>
            <Flex direction="column" align="stretch" gap={16} className={s.body}>
                <Flex direction="column" align="stretch" gap={4} className={s.summary}>
                    <Typography.Text variant="label" color="secondary">
                        {setTitle ? `${setTitle} · набор пройден` : 'На сегодня · всё повторено'}
                    </Typography.Text>
                    <Typography.Text variant="hero" asChild>
                        <h1 className={s.score}>
                            {correct} из {results.length}
                        </h1>
                    </Typography.Text>
                    <Typography.Text variant="body" color="secondary">
                        верных ответов
                    </Typography.Text>
                </Flex>

                {allRight ? (
                    <Flex align="center" gap={12} className={s.allRight}>
                        <IconCheck size={24} tone="positive" />
                        <Typography.Text variant="body">Без ошибок: все ответы верные</Typography.Text>
                    </Flex>
                ) : null}

                {facts.length > 0 ? (
                    <Flex direction="column" align="stretch" gap={8}>
                        <Typography.Text variant="label-strong" color="secondary" asChild>
                            <h2 className={s.sectionTitle}>Факты с ошибками</h2>
                        </Typography.Text>
                        <ul className={s.topics}>
                            {facts.map((fact) => (
                                <li key={fact.factId} className={s.topic}>
                                    <Flex justify="space-between" align="baseline" gap={12}>
                                        <Typography.Text variant="body" className={s.topicName}>
                                            {fact.factName}
                                        </Typography.Text>
                                        <Typography.Text variant="label" color="secondary" className={s.topicCount}>
                                            {errorsLabel(fact.wrong)} из {fact.total}
                                        </Typography.Text>
                                    </Flex>
                                    <div className={s.bar} aria-hidden="true">
                                        <div
                                            className={s.barFill}
                                            style={{ width: `${(fact.wrong / fact.total) * 100}%` }}
                                        />
                                    </div>
                                </li>
                            ))}
                        </ul>
                    </Flex>
                ) : null}

                {/* The date is a placeholder until the backend returns the
                    review schedule; with no errors there is nothing to return to. */}
                {correct < results.length ? (
                    <Flex align="center" gap={8} className={s.note}>
                        <IconCalendar size={20} tone="muted" />
                        <Typography.Text variant="body" color="secondary">
                            Вернёмся к сложным через 2 дня
                        </Typography.Text>
                    </Flex>
                ) : null}
            </Flex>

            <Flex direction="column" align="stretch" gap={8}>
                {onShare ? (
                    <Button size="medium" variant="primary" stretched onClick={onShare}>
                        Поделиться набором
                    </Button>
                ) : null}
                <Button
                    size="medium"
                    variant={onShare ? 'ghost' : 'secondary'}
                    stretched
                    onClick={onExit}
                >
                    {setTitle ? 'К набору' : 'На главную'}
                </Button>
            </Flex>
        </Screen>
    );
}

export default Result;
