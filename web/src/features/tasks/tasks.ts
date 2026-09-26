import type { Task, TaskState } from '@/api/types'
import type { StatusMarkKind } from '@/components/shared/status-mark'

// The board's order of states: under way, waiting to be merged, done.
export const taskStates: TaskState[] = ['running', 'merge', 'done']

// How many done tasks a column shows before the rest fold away.
export const shownDone = 5

// markOf is the mark a task wears: a request of it waiting on a person
// says so over its state.
export function markOf(task: Task): StatusMarkKind {
  if (task.waiting) return 'wait'
  return task.state === 'running' ? 'run' : task.state
}

// lastMoved is when a task last moved: now for one under way, else when it
// ended; what orders a column, the latest first.
function lastMoved(task: Task, now: number): number {
  return task.state === 'running' || !task.ended_at ? now : new Date(task.ended_at).getTime()
}

function latestFirst(now: number) {
  return (a: Task, b: Task) => lastMoved(b, now) - lastMoved(a, now) || b.started_at.localeCompare(a.started_at)
}

// byState puts tasks in the board's columns, each the latest first.
export function byState(tasks: Task[], now: number): Record<TaskState, Task[]> {
  const columns: Record<TaskState, Task[]> = { running: [], merge: [], done: [] }
  for (const task of tasks) columns[task.state].push(task)
  for (const state of taskStates) columns[state].sort(latestFirst(now))
  return columns
}

// byMember puts tasks in a column a member, the members in the order
// given: under way first, then waiting to be merged, then done, each the
// latest first. A member with no task has an empty column.
export function byMember(tasks: Task[], members: string[], now: number): Map<string, Task[]> {
  const columns = new Map<string, Task[]>(members.map((id) => [id, []]))
  for (const task of tasks) columns.get(task.member_id)?.push(task)
  const order = (a: Task, b: Task) => taskStates.indexOf(a.state) - taskStates.indexOf(b.state) || latestFirst(now)(a, b)
  for (const column of columns.values()) column.sort(order)
  return columns
}

// countStates counts a column's tasks by state; a column with a task
// waiting on a person counts its running ones as waiting.
export function countStates(tasks: Task[]): { kind: StatusMarkKind; state: TaskState; n: number }[] {
  const waiting = tasks.some((task) => task.waiting)
  return taskStates
    .map((state) => ({ state, n: tasks.filter((task) => task.state === state).length }))
    .filter(({ n }) => n > 0)
    .map(({ state, n }) => ({ state, n, kind: state === 'running' ? (waiting ? 'wait' : 'run') : state }))
}
