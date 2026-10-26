import { useRef, useState, type ChangeEvent } from 'react';
import { Button, Flex, Spinner, Typography } from '@maxhub/max-ui';
import { StatusScreen } from '../components/StatusScreen';
import { IconAlert, IconDoc } from '../components/Icons';
import { isNotesRunning, notesFailureText, notesFilesProblem, notesProgress } from '../utils/notes';
import { plural } from '../utils/plural';
import { useNotesJob } from '../utils/useNotesJob';
import s from './AddNote.module.css';

export interface AddNoteProps {
    onBack: () => void;
    /** The set is made: open it. */
    onDone: (setId: string) => void;
    /** Opened from home's row: notes that failed within the hour say why. */
    resume?: boolean;
}

const filesLabel = (count: number) => `${count} ${plural(count, ['файл', 'файла', 'файлов'])}`;

function sizeLabel(bytes: number): string {
    const mb = bytes / (1024 * 1024);
    return mb < 0.1 ? 'меньше 0,1 МБ' : `${mb.toFixed(1).replace('.', ',')} МБ`;
}

/** Leaving does not stop the notes: the server makes the set either way. */
const MAY_LEAVE = 'Можно выйти: набор появится на главной';

/**
 * The notes go to the same queue as the bot's, so the set comes out the
 * same. Where the mini app cannot send them, as outside MAX and Telegram,
 * the screen points to the bot as before.
 */
export function AddNote({ onBack, onDone, resume = false }: AddNoteProps) {
    const { access, job, followed, upload } = useNotesJob((done) => {
        if (done.state === 'done' && done.setId) {
            onDone(done.setId);
        }
    }, { followFailed: resume });
    const fileRef = useRef<HTMLInputElement>(null);
    const [sending, setSending] = useState<File[] | null>(null);
    const abort = useRef<AbortController | null>(null);
    // A refusal of the picked files; it shows over an older job's end.
    const [problem, setProblem] = useState('');
    const running = isNotesRunning(job);
    // Only notes followed here end here; a set made earlier is on home.
    const following = followed && !problem;

    const pick = () => fileRef.current?.click();

    const send = async (event: ChangeEvent<HTMLInputElement>) => {
        const files = Array.from(event.target.files ?? []);
        // The same files picked again must fire the change once more.
        event.target.value = '';
        if (files.length === 0 || sending) {
            return;
        }
        const tooMuch = notesFilesProblem(files);
        setProblem(tooMuch);
        if (tooMuch) {
            return;
        }
        setSending(files);
        abort.current = new AbortController();
        const result = await upload(files, abort.current.signal);
        abort.current = null;
        setSending(null);
        if (!result.ok) {
            setProblem(result.error);
        }
    };

    const input = (
        <input
            ref={fileRef}
            type="file"
            multiple
            // No accept: an Android webview may pass only its first type to
            // the picker, leaving photos or PDFs out. The server tells the
            // format by content and says when it cannot read a file.
            className={s.file}
            tabIndex={-1}
            aria-hidden="true"
            onChange={(event) => void send(event)}
        />
    );
    const toHome = (
        <Button size="medium" variant="ghost" stretched onClick={onBack}>
            На главную
        </Button>
    );
    // As big as the icons of the other states, so the title stays put.
    const spinner = (
        <span className={s.spinner}>
            <Spinner size={32} appearance="neutral-themed" />
        </span>
    );

    if (access === 'loading') {
        return <StatusScreen icon={spinner} title="Секунду" text="Готовим загрузку" />;
    }

    if (access === 'off') {
        return (
            <StatusScreen
                icon={<IconDoc size={48} tone="muted" />}
                title="Пришлите конспект боту"
                text="Отправьте в чат с ботом текст, PDF, Word, презентацию или фото страниц. Когда карточки будут готовы, набор появится здесь"
                action={
                    <Button size="medium" variant="primary" stretched onClick={onBack}>
                        Понятно
                    </Button>
                }
            />
        );
    }

    if (sending) {
        const bytes = sending.reduce((sum, file) => sum + file.size, 0);
        return (
            <StatusScreen
                icon={spinner}
                title="Отправляем конспект"
                text={`${filesLabel(sending.length)} · ${sizeLabel(bytes)}`}
                action={
                    <Button size="medium" variant="ghost" stretched onClick={() => abort.current?.abort()}>
                        Отмена
                    </Button>
                }
            />
        );
    }

    if (following && job && (running || job.state === 'done')) {
        const progress = job.state === 'done' ? { title: 'Набор готов', text: 'Открываем' } : notesProgress(job);
        return (
            <StatusScreen
                icon={spinner}
                title={progress.title}
                text={job.state === 'done' ? progress.text : `${progress.text}. ${MAY_LEAVE}`}
                action={job.state === 'done' ? undefined : toHome}
            />
        );
    }

    if (following && job?.state === 'failed') {
        return (
            <>
                {input}
                <StatusScreen
                    icon={<IconAlert size={48} tone="muted" />}
                    title="Не получилось сделать карточки"
                    text={notesFailureText(job)}
                    action={
                        <Flex direction="column" align="stretch" gap={8}>
                            <Button size="medium" variant="primary" stretched onClick={pick}>
                                Выбрать другой файл
                            </Button>
                            {toHome}
                        </Flex>
                    }
                />
            </>
        );
    }

    return (
        <>
            {input}
            <StatusScreen
                icon={<IconDoc size={48} tone="muted" />}
                title="Загрузите конспект или пришлите его боту в чат"
                text="PDF, Word, презентация, текст или фото страниц — до 20 МБ. Карточки будут готовы через пару минут"
                action={
                    <Flex direction="column" align="stretch" gap={8}>
                        {problem ? (
                            <Typography.Text variant="description" className={s.problem} role="alert">
                                {problem}
                            </Typography.Text>
                        ) : null}
                        <Button size="medium" variant="primary" stretched onClick={pick}>
                            Выбрать файлы
                        </Button>
                        <Button size="medium" variant="ghost" stretched onClick={onBack}>
                            Отмена
                        </Button>
                    </Flex>
                }
            />
        </>
    );
}

export default AddNote;
