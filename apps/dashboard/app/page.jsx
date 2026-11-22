import { connection } from 'next/server'
import Dashboard from './dashboard'

export default async function Page() {
  await connection()
  return <Dashboard />
}