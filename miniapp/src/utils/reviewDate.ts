import { plural } from './plural';

const WEEKDAY_PHRASES: Record<number, string> = {
    1: 'в понедельник',
    2: 'во вторник',
    3: 'в среду',
    4: 'в четверг',
    5: 'в пятницу',
    6: 'в субботу',
    0: 'в воскресенье',
};

const timeFormatter = new Intl.DateTimeFormat('ru-RU', {
    hour: '2-digit',
    minute: '2-digit',
});

const monthDayFormatter = new Intl.DateTimeFormat('ru-RU', {
    day: 'numeric',
    month: 'long',
});

/**
 * Formats the next review subtitle for todayCard when dueCount === 0.
 *
 * Rules:
 * - If nextReviewAt is null/empty or invalid: returns null
 * - Already due: «Следующий повтор сейчас»
 * - Within the hour: «Следующий повтор через 5 минут» (FSRS brings
 *   a fresh or a missed card back in minutes)
 * - Later today: «Следующий повтор сегодня в 18:30»
 * - Tomorrow: «Следующий повтор завтра»
 * - Later in the current calendar week (Mon–Sun): «Следующий повтор в/во [день недели]»
 * - On future weeks: «Следующий повтор [число] [месяца]» (e.g. «Следующий повтор 15 октября»)
 */
export function formatNextReviewSubtitle(
    nextReviewAt: string | null | undefined,
    now: Date = new Date(),
): string | null {
    if (!nextReviewAt) {
        return null;
    }

    const targetDate = new Date(nextReviewAt);
    if (isNaN(targetDate.getTime())) {
        return null;
    }

    const diffMs = targetDate.getTime() - now.getTime();
    if (diffMs <= 0) {
        return 'Следующий повтор сейчас';
    }

    const diffMinutes = Math.ceil(diffMs / 60_000);
    if (diffMinutes === 1) {
        return 'Следующий повтор через минуту';
    }
    if (diffMinutes < 60) {
        return `Следующий повтор через ${diffMinutes} ${plural(diffMinutes, ['минуту', 'минуты', 'минут'])}`;
    }

    const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const targetStart = new Date(targetDate.getFullYear(), targetDate.getMonth(), targetDate.getDate());

    const diffDays = Math.round((targetStart.getTime() - todayStart.getTime()) / (24 * 60 * 60 * 1000));

    if (diffDays <= 0) {
        return `Следующий повтор сегодня в ${timeFormatter.format(targetDate)}`;
    }

    if (diffDays === 1) {
        return 'Следующий повтор завтра';
    }

    // Current calendar week: Monday through Sunday
    const dayOfWeek = todayStart.getDay(); // 0 = Sunday, 1 = Monday, ...
    const daysUntilSunday = dayOfWeek === 0 ? 0 : 7 - dayOfWeek;
    const endOfWeekStart = new Date(
        todayStart.getFullYear(),
        todayStart.getMonth(),
        todayStart.getDate() + daysUntilSunday,
    );

    if (targetStart.getTime() <= endOfWeekStart.getTime()) {
        const phrase = WEEKDAY_PHRASES[targetStart.getDay()];
        if (phrase) {
            return `Следующий повтор ${phrase}`;
        }
    }

    return `Следующий повтор ${monthDayFormatter.format(targetDate)}`;
}

/** Whether the next review falls on the same calendar day as now. */
export function isNextReviewToday(nextReviewAt: string | null | undefined, now: Date = new Date()): boolean {
    if (!nextReviewAt) {
        return false;
    }
    const target = new Date(nextReviewAt);
    return (
        !isNaN(target.getTime()) &&
        target.getFullYear() === now.getFullYear() &&
        target.getMonth() === now.getMonth() &&
        target.getDate() === now.getDate()
    );
}
