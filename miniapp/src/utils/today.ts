const dayFormat = new Intl.DateTimeFormat('ru-RU', { weekday: 'long', day: 'numeric', month: 'long' });

/** «Суббота, 18 октября»: the day the home screen is about. */
export function formatTodayDate(now: Date = new Date()): string {
    const text = dayFormat.format(now);
    return text.charAt(0).toUpperCase() + text.slice(1);
}
