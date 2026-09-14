import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { INITIAL_MOCK_COMPOSITIONS } from "../../lib/mock-data";
import { CompositionCard } from "./CompositionCard";

afterEach(cleanup);
it("opens a composition from its title using the keyboard", async () => {
  const user = userEvent.setup();
  const composition = INITIAL_MOCK_COMPOSITIONS[0];
  const inspect = vi.fn();
  render(
    <CompositionCard
      composition={composition}
      onInspect={inspect}
      onUpdate={vi.fn()}
      onDestroy={vi.fn()}
    />,
  );
  await user.tab();
  expect(document.activeElement).toBe(
    screen.getByRole("button", { name: composition.name }),
  );
  await user.keyboard("{Enter}");
  expect(inspect).toHaveBeenCalledWith(composition);
});
