import { Button, Flex } from '@maxhub/max-ui';
import { StatusScreen } from '../components/StatusScreen';
import { IconDoc } from '../components/Icons';

export interface AddNoteProps {
    onBack: () => void;
}

/**
 * Конспект принимает бот, а не мини-приложение — так в концепте.
 * Поэтому экран объясняет, куда идти, и не просит файл.
 * TODO: когда подключим SDK MAX, добавить кнопку перехода в чат с ботом.
 */
export function AddNote({ onBack }: AddNoteProps) {
    return (
        <StatusScreen
            icon={<IconDoc size={48} tone="muted" />}
            title="Конспект отправляют боту"
            text="Пришлите ему PDF или текст в чат — он соберёт карточки и пришлёт готовый набор сюда"
            action={
                <Flex direction="column" align="stretch" gap={8}>
                    <Button size="medium" variant="primary" stretched onClick={onBack}>
                        Понятно
                    </Button>
                </Flex>
            }
        />
    );
}

export default AddNote;
