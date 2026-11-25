import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import { ErrorBoundary } from './ErrorBoundary';

function ThrowingChild({ shouldThrow }: { shouldThrow: boolean }) {
  if (shouldThrow) {
    throw new Error('simulated crash');
  }
  return <div>Recovered view</div>;
}

beforeEach(() => {
  localStorage.setItem('opspilot-locale', 'en-US');
  vi.spyOn(console, 'error').mockImplementation(() => {});
});

it('catches rendering errors and recovers when Retried after the fault clears', async () => {
  const user = userEvent.setup();
  let fail = true;
  function ToggleFault() {
    if (fail) throw new Error('simulated crash');
    return <div>Healthy content</div>;
  }

  render(
    <ErrorBoundary>
      <ToggleFault />
    </ErrorBoundary>,
  );

  expect(screen.getByRole('alert')).toBeInTheDocument();
  expect(screen.getByText('Something went wrong')).toBeInTheDocument();
  expect(screen.getByText('simulated crash')).toBeInTheDocument();

  fail = false;
  await user.click(screen.getByRole('button', { name: 'Retry' }));
  expect(screen.getByText('Healthy content')).toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

it('resets error state automatically when resetKey changes', () => {
  const { rerender } = render(
    <ErrorBoundary resetKey="/broken">
      <ThrowingChild shouldThrow />
    </ErrorBoundary>,
  );

  expect(screen.getByRole('alert')).toBeInTheDocument();

  rerender(
    <ErrorBoundary resetKey="/healthy">
      <ThrowingChild shouldThrow={false} />
    </ErrorBoundary>,
  );

  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(screen.getByText('Recovered view')).toBeInTheDocument();
});
