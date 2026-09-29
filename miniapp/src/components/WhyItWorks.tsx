import { CellHeader, CellList, CellSimple, Flex, Typography } from '@maxhub/max-ui';
import s from './WhyItWorks.module.css';

interface Fact {
    title: string;
    text: string;
    source: string;
}

const FACTS: Fact[] = [
    {
        title: 'Вспоминать полезнее, чем перечитывать',
        text: 'Через неделю студенты, которые проверяли себя, вспомнили 61% текста, а те, кто перечитывал, — 40%.',
        source: 'Roediger, Karpicke · Psychological Science, 2006',
    },
    {
        title: 'Паузы важнее количества',
        text: 'Разнесённые по дням повторения запоминаются лучше, чем подряд. Чем дальше экзамен, тем длиннее могут быть паузы.',
        source: 'Cepeda и др. · обзор 317 экспериментов, 2006',
    },
    {
        title: 'Повтор — когда начинаете забывать',
        text: 'Карточка возвращается, когда вероятность её вспомнить падает примерно до 90%. Так каждое повторение укрепляет память сильнее.',
        source: 'Open Spaced Repetition',
    },
];

export function WhyItWorks() {
    return (
        <Flex direction="column" align="stretch" gap={8}>
            <CellHeader>Почему это работает</CellHeader>
            <CellList mode="island" filled className={s.factsList}>
                {FACTS.map((fact, index) => (
                    <CellSimple
                        key={fact.title}
                        separator={index > 0}
                        title={fact.title}
                        subtitle={
                            <Flex direction="column" align="stretch" gap={4}>
                                <Typography.Text
                                    variant="description"
                                    color="secondary"
                                    className={s.factText}
                                >
                                    {fact.text}
                                </Typography.Text>
                                <Typography.Text variant="label" color="tertiary">
                                    {fact.source}
                                </Typography.Text>
                            </Flex>
                        }
                    />
                ))}
            </CellList>
        </Flex>
    );
}
