import { describe, expect, test } from 'bun:test';
import { centerSquare } from './avatar';

describe('centerSquare', () => {
    test('cuts the sides of a wide photo', () => {
        expect(centerSquare(1600, 900)).toEqual({ x: 350, y: 0, side: 900 });
    });

    test('cuts the top and bottom of a tall photo', () => {
        expect(centerSquare(300, 500)).toEqual({ x: 0, y: 100, side: 300 });
    });

    test('keeps a square whole', () => {
        expect(centerSquare(512, 512)).toEqual({ x: 0, y: 0, side: 512 });
    });
});
