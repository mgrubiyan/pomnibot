import { Button, Flex } from '@maxhub/max-ui';
import { StatusScreen } from '../components/StatusScreen';
import { IconDoc } from '../components/Icons';
import { useBackButton } from '../max/useBackButton';

export interface AddNoteProps {
    onBack: () => void;
}

/**
 * Notes are taken by the bot, not by the mini app — that is the concept.
 * So this screen points the way instead of asking for a file.
 * TODO: once the MAX SDK is wired up, add a button that opens the bot chat.
 */
export function AddNote({ onBack }: AddNoteProps) {
    useBackButton(onBack);

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
