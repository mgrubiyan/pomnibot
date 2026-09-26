import { useEffect, useRef, useState } from 'react';
import { Button, Flex, IconButton, Input, Typography } from '@maxhub/max-ui';
import type { CardSet } from '../types';
import { mockToday } from '../mocks';
import { Screen } from '../components/Screen';
import { IconChevronLeft, IconCross } from '../components/Icons';
import { useBackButton } from '../max/useBackButton';
import { CODE_LENGTH, formatCode } from '../utils/code';
import s from './JoinSet.module.css';

const CHECK_DELAY = 700;

const digitsOf = (value: string) => value.replace(/\D/g, '').slice(0, CODE_LENGTH);

/**
 * Mocks instead of a request; the real endpoint comes later:
 * for now the code is looked up among the sets that carry one.
 */
function findSetByCode(code: string): Promise<CardSet> {
    return new Promise((resolve, reject) => {
        window.setTimeout(() => {
            const found = mockToday.sets.find((set) => set.shareCode === code);
            if (found) {
                resolve(found);
                return;
            }
            reject(new Error('not found'));
        }, CHECK_DELAY);
    });
}

export interface JoinSetProps {
    /** Code from an invite link; the student still confirms it. */
    initialCode?: string;
    onBack: () => void;
    onOpenSet: (setId: string) => void;
}

export function JoinSet({ initialCode = '', onBack, onOpenSet }: JoinSetProps) {
    const [digits, setDigits] = useState(initialCode);
    const [checking, setChecking] = useState(false);
    const [notFound, setNotFound] = useState(false);
    // The check outlives the screen when the user backs out mid-way;
    // its answer must not pull them onto the set screen after that.
    const mounted = useRef(false);

    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
        };
    }, []);

    useBackButton(onBack);

    const ready = digits.length === CODE_LENGTH;

    const change = (value: string) => {
        setDigits(digitsOf(value));
        setNotFound(false);
    };

    const submit = () => {
        if (!ready || checking) {
            return;
        }
        setChecking(true);
        setNotFound(false);

        findSetByCode(digits)
            .then((set) => {
                if (mounted.current) {
                    onOpenSet(set.id);
                }
            })
            .catch(() => {
                if (!mounted.current) {
                    return;
                }
                setChecking(false);
                setNotFound(true);
            });
    };

    return (
        <Screen>
            <Flex align="center" gap={8} className={s.header}>
                <IconButton size="small" variant="ghost" aria-label="Назад" onClick={onBack}>
                    <IconChevronLeft size={20} />
                </IconButton>
                <Typography.Text variant="subheader" asChild>
                    <h1 className={s.title}>Код набора</h1>
                </Typography.Text>
            </Flex>

            <Flex direction="column" align="stretch" gap={12} className={s.body}>
                <Typography.Text variant="body" color="secondary" className={s.hint}>
                    Введите код, который прислали в чате группы. Набор появится в списке ваших.
                </Typography.Text>

                <Input
                    size="large"
                    value={formatCode(digits)}
                    placeholder="000 000"
                    inputMode="numeric"
                    autoComplete="off"
                    aria-label="Код набора, шесть цифр"
                    disabled={checking}
                    innerClassNames={{ input: s.codeInput }}
                    onChange={(event) => change(event.target.value)}
                    onKeyDown={(event) => {
                        if (event.key === 'Enter') {
                            submit();
                        }
                    }}
                />

                {notFound ? (
                    <Flex align="center" gap={8}>
                        <IconCross size={20} tone="negative" />
                        <Typography.Text variant="description" color="secondary">
                            Набора с таким кодом нет. Проверьте цифры.
                        </Typography.Text>
                    </Flex>
                ) : null}
            </Flex>

            <Flex direction="column" align="stretch" gap={8}>
                <Button
                    size="medium"
                    variant="primary"
                    stretched
                    disabled={!ready}
                    loading={checking}
                    onClick={submit}
                >
                    Добавить набор
                </Button>
            </Flex>
        </Screen>
    );
}

export default JoinSet;
