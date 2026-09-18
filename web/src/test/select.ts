import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

// pickOption chooses an option in a shadcn Select the way a person does:
// open the trigger its label points at, click the option by name.
export async function pickOption(label: string, option: string | RegExp) {
  await userEvent.click(screen.getByLabelText(label))
  await userEvent.click(await screen.findByRole('option', { name: option }))
}
