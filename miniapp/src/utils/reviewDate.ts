const WEEKDAY_PHRASES: Record<number, string> = {
    1: 'в понедельник',
    2: 'во вторник',
    3: 'в среду',
    4: 'в четверг',
    5: 'в пятницу',
    6: 'в субботу',
    0: 'в воскресенье',
};

const monthDayFormatter = new Intl.DateTimeFormat('ru-RU', {
    day: 'numeric',
    month: 'long',
});

/**
 * Formats the next review subtitle for todayCard when dueCount === 0.
 *
 * Rules:
 * - If nextReviewAt is null/empty or invalid: returns null
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

    const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const targetStart = new Date(targetDate.getFullYear(), targetDate.getMonth(), targetDate.getDate());

    const diffDays = Math.round((targetStart.getTime() - todayStart.getTime()) / (24 * 60 * 60 * 1000));

    if (diffDays <= 0) {
        return 'Следующий повтор сегодня';
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
