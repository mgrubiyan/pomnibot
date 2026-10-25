import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Button, CellHeader, CellList, CellSimple, Flex, Spinner, Typography } from '@maxhub/max-ui';
import type { SetMistakes as Mistakes } from '../types';
import { api } from '../api';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { StatusScreen } from '../components/StatusScreen';
import { IconOffline } from '../components/Icons';
import { formatAuthorName } from '../utils/user';
import { factLabel, factStudents, mistakesLabel, studentFacts } from '../utils/mistakes';
import { plural } from '../utils/plural';
import s from './SetMistakes.module.css';

type Status = 'loading' | 'error' | 'forbidden' | 'missing' | 'ready';

/** A member or a fact opened from the overview; only those with answers open. */
type Focus = { kind: 'student'; id: string } | { kind: 'fact'; id: string };

export interface SetMistakesProps {
    setId: string;
    setTitle?: string;
    onBack: () => void;
}

const SPINNER_DELAY_MS = 200;

/** Rows without mistakes step back, so the eye lands on the ones with them. */
const quiet = { title: s.quiet };

const factsLabel = (count: number) => `${count} ${plural(count, ['факт', 'факта', 'фактов'])}`;
const studentsLabel = (count: number) =>
    `${count} ${plural(count, ['участник', 'участника', 'участников'])}`;

/** Where the members of the author's set go wrong: the facts that trip up
    the most of them, and every member's own weak facts. */
export function SetMistakes({ setId, setTitle, onBack }: SetMistakesProps) {
    const [status, setStatus] = useState<Status>('loading');
    const [data, setData] = useState<Mistakes | null>(null);
    const [focus, setFocus] = useState<Focus | null>(null);
    const [attempt, setAttempt] = useState(0);
    // A quick answer would only flash the spinner, so it waits a moment.
    const [slow, setSlow] = useState(false);
    // The overview unmounts while a member or a fact is open; coming back
    // puts the list where it was.
    const bodyRef = useRef<HTMLDivElement>(null);
    const overviewScroll = useRef(0);

    useLayoutEffect(() => {
        if (!focus && bodyRef.current) {
            bodyRef.current.scrollTop = overviewScroll.current;
        }
    }, [focus]);

    const open = (next: Focus) => {
        overviewScroll.current = bodyRef.current?.scrollTop ?? 0;
        setFocus(next);
    };

    useEffect(() => {
        let cancelled = false;

        api.GET('/sets/{setId}/mistakes', {
            params: { path: { setId } },
        })
            .then(({ data: next, response }) => {
                if (cancelled) {
                    return;
                }
                if (response.status === 403) {
                    setStatus('forbidden');
                    return;
                }
                if (response.status === 404) {
                    setStatus('missing');
                    return;
                }
                if (!next) {
                    setStatus('error');
                    return;
                }
                setData(next);
                setStatus('ready');
            })
            .catch(() => {
                if (!cancelled) {
                    setStatus('error');
                }
            });

        return () => {
            cancelled = true;
        };
    }, [attempt, setId]);

    useEffect(() => {
        if (status !== 'loading') {
            return;
        }
        const timer = setTimeout(() => setSlow(true), SPINNER_DELAY_MS);
        return () => clearTimeout(timer);
    }, [status]);

    const retry = () => {
        setSlow(false);
        setStatus('loading');
        setAttempt((current) => current + 1);
    };

    if (status === 'loading' && !slow) {
        return (
            <Screen>
                <ScreenHeader title="Ошибки участников" subtitle={setTitle} onBack={onBack} />
            </Screen>
        );
    }

    if (status === 'loading') {
        return (
            <StatusScreen
                icon={<Spinner size={32} appearance="neutral-themed" />}
                title="Загрузка ошибок"
                text="Считаем ответы участников"
            />
        );
    }

    if (status === 'forbidden') {
        return (
            <StatusScreen
                icon={<IconOffline size={48} tone="muted" />}
                title="Доступ ограничен"
                text="Ошибки участников видит только автор набора"
                action={
                    <Button size="medium" variant="primary" stretched onClick={onBack}>
                        Назад
                    </Button>
                }
            />
        );
    }

    if (status === 'missing') {
        return (
            <StatusScreen
                icon={<IconOffline size={48} tone="muted" />}
                title="Набор не найден"
                text="Возможно, его удалили"
                action={
                    <Button size="medium" variant="primary" stretched onClick={onBack}>
                        Назад
                    </Button>
                }
            />
        );
    }

    if (status === 'error' || !data) {
        return (
            <StatusScreen
                icon={<IconOffline size={48} tone="muted" />}
                title="Не удалось загрузить"
                text="Проверьте соединение и попробуйте снова"
                action={
                    <Flex direction="column" align="stretch" gap={8}>
                        <Button size="medium" variant="primary" stretched onClick={retry}>
                            Повторить
                        </Button>
                        <Button size="medium" variant="ghost" stretched onClick={onBack}>
                            Назад
                        </Button>
                    </Flex>
                }
            />
        );
    }

    const student = focus?.kind === 'student' ? data.students.find((item) => item.user.id === focus.id) : undefined;
    if (student) {
        const { answered, unanswered } = studentFacts(data, student.user.id);
        return (
            <Screen>
                <ScreenHeader
                    title={formatAuthorName(student.user)}
                    subtitle={mistakesLabel(student.mistakes, student.answers)}
                    onBack={() => setFocus(null)}
                />
                <Flex direction="column" align="stretch" gap={8} className={s.body}>
                    <CellList mode="island" filled className={s.list}>
                        {answered.map((entry, index) => (
                            <CellSimple
                                key={entry.fact.factId}
                                separator={index > 0}
                                innerClassNames={entry.mistakes === 0 ? quiet : undefined}
                                title={<span className={s.wrap}>{entry.fact.factName}</span>}
                                subtitle={mistakesLabel(entry.mistakes, entry.answers)}
                            />
                        ))}
                    </CellList>
                    {unanswered > 0 ? (
                        <Typography.Text variant="description" color="secondary" className={s.hint}>
                            Без ответов: {factsLabel(unanswered)}
                        </Typography.Text>
                    ) : null}
                </Flex>
            </Screen>
        );
    }

    const fact = focus?.kind === 'fact' ? data.facts.find((item) => item.factId === focus.id) : undefined;
    if (fact) {
        const { answered, unanswered } = factStudents(data, fact);
        return (
            <Screen>
                <ScreenHeader title={fact.factName} subtitle={factLabel(fact)} onBack={() => setFocus(null)} />
                <Flex direction="column" align="stretch" gap={8} className={s.body}>
                    <CellList mode="island" filled className={s.list}>
                        {answered.map((entry, index) => (
                            <CellSimple
                                key={entry.user.id}
                                separator={index > 0}
                                innerClassNames={entry.mistakes === 0 ? quiet : undefined}
                                title={<span className={s.wrap}>{formatAuthorName(entry.user)}</span>}
                                subtitle={mistakesLabel(entry.mistakes, entry.answers)}
                            />
                        ))}
                    </CellList>
                    {unanswered > 0 ? (
                        <Typography.Text variant="description" color="secondary" className={s.hint}>
                            Без ответов: {studentsLabel(unanswered)}
                        </Typography.Text>
                    ) : null}
                </Flex>
            </Screen>
        );
    }

    return (
        <Screen>
            <ScreenHeader title="Ошибки участников" subtitle={setTitle} onBack={onBack} />

            {data.students.length === 0 ? (
                <div className={s.empty}>
                    <Typography.Text color="secondary">
                        Пока никто не присоединился. Поделитесь набором — здесь появится, на каких фактах участники ошибаются.
                    </Typography.Text>
                </div>
            ) : (
                <Flex ref={bodyRef} direction="column" align="stretch" gap={16} className={s.body}>
                    <Typography.Text variant="description" color="secondary" className={s.hint}>
                        Сверху — факты, на которых ошибалось больше всего участников. Ваши ответы не считаются.
                    </Typography.Text>

                    <Flex direction="column" align="stretch" gap={8}>
                        <CellHeader titleStyle="normal">Факты · {data.facts.length}</CellHeader>
                        {data.facts.length === 0 ? (
                            <Typography.Text variant="description" color="secondary" className={s.hint}>
                                В наборе пока нет фактов
                            </Typography.Text>
                        ) : (
                            <CellList mode="island" filled className={s.list}>
                                {data.facts.map((item, index) => (
                                    <CellSimple
                                        key={item.factId}
                                        separator={index > 0}
                                        innerClassNames={item.studentsMistaken === 0 ? quiet : undefined}
                                        as={item.studentsAnswered > 0 ? 'button' : undefined}
                                        showChevron={item.studentsAnswered > 0}
                                        onClick={
                                            item.studentsAnswered > 0
                                                ? () => open({ kind: 'fact', id: item.factId })
                                                : undefined
                                        }
                                        title={<span className={s.wrap}>{item.factName}</span>}
                                        subtitle={factLabel(item)}
                                    />
                                ))}
                            </CellList>
                        )}
                    </Flex>

                    <Flex direction="column" align="stretch" gap={8}>
                        <CellHeader titleStyle="normal">Участники · {data.students.length}</CellHeader>
                        <CellList mode="island" filled className={s.list}>
                            {data.students.map((item, index) => (
                                <CellSimple
                                    key={item.user.id}
                                    separator={index > 0}
                                    innerClassNames={item.mistakes === 0 ? quiet : undefined}
                                    as={item.answers > 0 ? 'button' : undefined}
                                    showChevron={item.answers > 0}
                                    onClick={
                                        item.answers > 0
                                            ? () => open({ kind: 'student', id: item.user.id })
                                            : undefined
                                    }
                                    title={<span className={s.wrap}>{formatAuthorName(item.user)}</span>}
                                    subtitle={mistakesLabel(item.mistakes, item.answers)}
                                />
                            ))}
                        </CellList>
                    </Flex>
                </Flex>
            )}
        </Screen>
    );
}

export default SetMistakes;
