import { useEffect, useState } from 'react';

const KONAMI_CODE = [
  'ArrowUp',
  'ArrowUp',
  'ArrowDown',
  'ArrowDown',
  'ArrowLeft',
  'ArrowRight',
  'ArrowLeft',
  'ArrowRight',
  'b',
  'a',
];

export function useKonamiCode(): [boolean, (value: boolean) => void] {
  const [triggered, setTriggered] = useState(() => {
    return localStorage.getItem('matrixMode') === 'true';
  });
  const [index, setIndex] = useState(0);

  useEffect(() => {
    localStorage.setItem('matrixMode', String(triggered));
  }, [triggered]);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === KONAMI_CODE[index]) {
        const nextIndex = index + 1;
        if (nextIndex === KONAMI_CODE.length) {
          setTriggered((prev) => !prev);
          setIndex(0);
        } else {
          setIndex(nextIndex);
        }
      } else {
        setIndex(0);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [index]);

  return [triggered, setTriggered];
}
