import { Avatar, CellList, CellSimple, Flex, Typography } from '@maxhub/max-ui';
import { Screen } from '../components/Screen';
import { ScreenHeader } from '../components/ScreenHeader';
import { IconDoc, IconPlus } from '../components/Icons';
import s from './CreateSet.module.css';

export interface CreateSetProps {
    onFromNotes: () => void;
    onManual: () => void;
    onBack: () => void;
}

/**
 * Two ways to a new set; the notes come first, they are what Помнибот is for.
 * They are actions, so they sit at the bottom under the thumb, like the
 * main button on every other screen.
 */
export function CreateSet({ onFromNotes, onManual, onBack }: CreateSetProps) {
    return (
        <Screen>
            <ScreenHeader title="Новый набор" onBack={onBack} />
            <Flex direction="column" align="stretch" justify="end" gap={12} className={s.body}>
                <Typography.Text variant="body" color="secondary" className={s.hint}>
                    Как соберём карточки?
                </Typography.Text>
                <CellList mode="island" filled className={s.list}>
                    <CellSimple
                        as="button"
                        showChevron
                        onClick={onFromNotes}
                        before={
                            <Avatar.Container size={44} form="squircle">
                                <Avatar.Text gradient="blue">
                                    <IconDoc size={24} />
                                </Avatar.Text>
                            </Avatar.Container>
                        }
                        innerClassNames={{ before: s.icon }}
                        title="Из конспекта"
                        subtitle="PDF, Word, слайды или фото страниц — карточки соберёт бот"
                    />
                    <CellSimple
                        as="button"
                        separator
                        showChevron
                        onClick={onManual}
                        before={
                            <Avatar.Container size={44} form="squircle">
                                <Avatar.Text gradient="purple">
                                    <IconPlus size={24} />
                                </Avatar.Text>
                            </Avatar.Container>
                        }
                        innerClassNames={{ before: s.icon }}
                        title="Вручную"
                        subtitle="Составьте карточки сами: вопрос, ответ и варианты"
                    />
                </CellList>
            </Flex>
        </Screen>
    );
}

export default CreateSet;
