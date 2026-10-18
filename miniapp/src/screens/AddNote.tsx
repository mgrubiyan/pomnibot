import { Button, Flex } from '@maxhub/max-ui';
import { StatusScreen } from '../components/StatusScreen';
import { IconDoc } from '../components/Icons';

export interface AddNoteProps {
    onBack: () => void;
}

/**
 * Notes are taken by the bot, not by the mini app — that is the concept.
 * So this screen points the way instead of asking for a file.
 * TODO: once the MAX SDK is wired up, add a button that opens the bot chat.
 */
export function AddNote({ onBack }: AddNoteProps) {
    return (
        <StatusScreen
            icon={<IconDoc size={48} tone="muted" />}
            title="Пришлите конспект боту"
            text="Отправьте в чат с ботом текст, PDF, Word, презентацию или фото страниц. Когда карточки будут готовы, набор появится здесь"
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
