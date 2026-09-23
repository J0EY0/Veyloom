import { SearchIcon } from 'lucide-react'
import { InputGroup, InputGroupAddon, InputGroupInput } from '@/components/ui/input-group'

// SearchBox narrows one of the library's lists as you type.
export function SearchBox({ value, onChange, label }: { value: string; onChange: (value: string) => void; label: string }) {
  return (
    <InputGroup className="h-8 w-60 min-w-0">
      <InputGroupAddon align="inline-start">
        <SearchIcon className="size-3.5 text-subtle" />
      </InputGroupAddon>
      <InputGroupInput
        type="search"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-label={label}
        placeholder={label}
        className="text-xs [&::-webkit-search-cancel-button]:appearance-none"
      />
    </InputGroup>
  )
}
