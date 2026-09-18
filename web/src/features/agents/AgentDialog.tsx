import { useId, useState, type FormEvent, type ReactNode } from 'react'
import { useCreateAgent, useUpdateAgent, useMachines } from '@/api/agents'
import type { Agent, PermissionPreset, Machine } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { presetLabel, presets } from '@/features/members/presets'
import { detectedRuntimes } from '@/features/machines/machines'
import { runtimeName, runtimeRank } from '@/lib/runtimes'
import { useT } from '@/lib/i18n'
import { AvatarField } from './AvatarField'
import { projectsInUse } from './inUse'

export interface AgentDialogProps {
  // Absent when creating a new agent.
  agent?: Agent
  onClose: () => void
}

// Creates or edits one agent. An agent is set up on one machine's runtime:
// the machine comes first and the runtime is one that machine found. An
// agent still in a project stays on its machine, since its members run
// there. The role card is the big field.
export function AgentDialog({ agent, onClose }: AgentDialogProps) {
  const machines = useMachines()
  const create = useCreateAgent()
  const update = useUpdateAgent()
  const [error, setError] = useState<string>()
  const [optionsError, setOptionsError] = useState<string>()
  const [pickedMachine, setPickedMachine] = useState(agent?.machine_id ?? '')
  const [pickedRuntime, setPickedRuntime] = useState(agent?.runtime ?? '')
  const [avatar, setAvatar] = useState(agent?.avatar ?? '')
  const [uploading, setUploading] = useState(false)
  const pending = create.isPending || update.isPending
  const id = useId()
  const t = useT()

  const online = machines.data ?? []
  // A new agent starts on the first connected machine until one is picked.
  const machineId = pickedMachine || online[0]?.id || ''
  const machineOptions = machineChoices(online, agent, t)
  const runtimeIds = runtimeChoices(
    online.find((machine) => machine.id === machineId),
    agent,
    machineId,
  )
  const runtime = runtimeIds.includes(pickedRuntime) ? pickedRuntime : (runtimeIds[0] ?? '')
  const locked = agent !== undefined && agent.projects.length > 0

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const name = String(data.get('name') ?? '').trim()
    if (name === '') {
      setError(t('agent.nameRequired'))
      return
    }
    let options: Record<string, unknown> = {}
    const raw = String(data.get('runtime_options') ?? '').trim()
    if (raw !== '') {
      try {
        const parsed: unknown = JSON.parse(raw)
        if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('not an object')
        options = parsed as Record<string, unknown>
      } catch {
        setOptionsError(t('agent.optionsInvalid'))
        return
      }
    }
    setError(undefined)
    setOptionsError(undefined)
    const body = {
      name,
      avatar,
      machine_id: machineId,
      runtime,
      model: String(data.get('model') ?? '').trim(),
      role_card: String(data.get('role_card') ?? ''),
      permission_preset: String(data.get('permission_preset') ?? 'read_only') as PermissionPreset,
      runtime_options: options,
    }
    const done = {
      onSuccess: onClose,
      // Moved while it was still in a project, since this dialog opened.
      onError: (err: Error) => {
        const projects = projectsInUse(err)
        setError(projects.length > 0 ? t('agent.machineLocked', { projects: projects.join('、') }) : err.message)
      },
    }
    if (agent) {
      update.mutate({ id: agent.id, ...body }, done)
    } else {
      create.mutate(body, done)
    }
  }

  return (
    <Dialog open onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-xl" aria-describedby={undefined}>
        <form onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{agent ? agent.name : t('agents.new')}</DialogTitle>
          </DialogHeader>
          <FieldGroup className="my-4 gap-4">
            <div className="flex items-start gap-4">
              <AvatarField avatar={avatar} runtime={runtime || (agent?.runtime ?? '')} onChange={setAvatar} onBusyChange={setUploading} />
              <Field data-invalid={error ? true : undefined}>
                <FieldLabel htmlFor={`${id}-name`}>{t('agent.name')}</FieldLabel>
                <Input
                  id={`${id}-name`}
                  name="name"
                  defaultValue={agent?.name ?? ''}
                  autoComplete="off"
                  spellCheck={false}
                  aria-invalid={error ? true : undefined}
                />
                {error ? <FieldError>{error}</FieldError> : null}
              </Field>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <Field>
                <FieldLabel htmlFor={`${id}-machine`}>{t('agent.machine')}</FieldLabel>
                <Select value={machineId} onValueChange={setPickedMachine} disabled={locked || machineOptions.length === 0}>
                  <WhyLocked reason={locked ? t('agent.machineLocked', { projects: agent.projects.join('、') }) : undefined}>
                    <SelectTrigger id={`${id}-machine`} className="w-full">
                      <SelectValue
                        placeholder={machines.isPending ? t('common.loading') : machineOptions.length === 0 ? t('agent.noMachines') : t('agent.pickMachine')}
                      />
                    </SelectTrigger>
                  </WhyLocked>
                  <SelectContent>
                    {machineOptions.map((option) => (
                      <SelectItem key={option.id} value={option.id}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor={`${id}-runtime`}>{t('agent.runtime')}</FieldLabel>
                <Select value={runtime} onValueChange={setPickedRuntime} disabled={runtimeIds.length === 0}>
                  <SelectTrigger id={`${id}-runtime`} className="w-full">
                    <SelectValue
                      placeholder={machines.isPending ? t('common.loading') : machineId === '' ? t('agent.pickMachineFirst') : t('agent.noRuntimes')}
                    />
                  </SelectTrigger>
                  <SelectContent>
                    {runtimeIds.map((name) => (
                      <SelectItem key={name} value={name}>
                        {runtimeName(name)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <Field>
                <FieldLabel htmlFor={`${id}-model`}>{t('agent.model')}</FieldLabel>
                <Input
                  id={`${id}-model`}
                  name="model"
                  defaultValue={agent?.model ?? ''}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={t('agent.modelPlaceholder')}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor={`${id}-permission`}>{t('agent.permission')}</FieldLabel>
                <Select name="permission_preset" defaultValue={agent?.permission_preset ?? 'read_only'}>
                  <SelectTrigger id={`${id}-permission`} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {presets.map((p) => (
                      <SelectItem key={p} value={p}>
                        {presetLabel(p)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
            <Field>
              <FieldLabel htmlFor={`${id}-role`}>{t('agent.roleCard')}</FieldLabel>
              <Textarea
                id={`${id}-role`}
                name="role_card"
                rows={8}
                defaultValue={agent?.role_card ?? ''}
                placeholder={t('agent.roleCardPlaceholder')}
                spellCheck={false}
                className="min-h-40 leading-[1.55]"
              />
            </Field>
            <Field data-invalid={optionsError ? true : undefined}>
              <FieldLabel htmlFor={`${id}-options`}>{t('agent.options')}</FieldLabel>
              <Textarea
                id={`${id}-options`}
                name="runtime_options"
                rows={3}
                defaultValue={agent?.runtime_options && Object.keys(agent.runtime_options).length > 0 ? JSON.stringify(agent.runtime_options, null, 2) : ''}
                placeholder='{"approval": true}'
                spellCheck={false}
                aria-invalid={optionsError ? true : undefined}
                className="font-mono text-[0.78125rem]"
              />
              {optionsError ? <FieldError>{optionsError}</FieldError> : null}
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={pending || uploading || machineId === '' || runtime === ''} aria-busy={pending}>
              {pending ? t('common.saving') : agent ? t('common.save') : t('agent.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// The machines an agent can be set up on: the connected ones, and the one
// an agent being edited is on when that one is not connected now.
function machineChoices(online: Machine[], agent: Agent | undefined, t: ReturnType<typeof useT>): { id: string; label: string }[] {
  const machines = online.map((machine) => ({ id: machine.id, label: machine.name }))
  if (agent && !online.some((machine) => machine.id === agent.machine_id)) {
    machines.push({ id: agent.machine_id, label: t('agent.machineOffline', { name: agent.machine_name }) })
  }
  return machines
}

// The runtimes to pick from on a machine: what it found installed, the test
// runtime left out, in the usual order. An agent being edited on that
// machine keeps the runtime it names, even one the machine no longer
// reports or cannot be asked about while it is offline.
function runtimeChoices(machine: Machine | undefined, agent: Agent | undefined, machineId: string): string[] {
  const found = machine ? detectedRuntimes(machine).map((runtime) => runtime.info.name) : []
  const kept = agent && agent.machine_id === machineId ? [agent.runtime] : []
  return [...new Set([...found, ...kept])].sort((a, b) => runtimeRank(a) - runtimeRank(b))
}

// A disabled field says why on hover: a disabled control gets no pointer
// events, so the tooltip hangs on a wrapper.
function WhyLocked({ reason, children }: { reason?: string; children: ReactNode }) {
  if (!reason) return children
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="block" tabIndex={0}>
          {children}
        </span>
      </TooltipTrigger>
      <TooltipContent>{reason}</TooltipContent>
    </Tooltip>
  )
}
