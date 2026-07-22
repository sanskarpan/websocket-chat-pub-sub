import type { Metadata } from 'next'
import './globals.css'

export const metadata: Metadata = {
  title: 'WebSocket Chat',
  description: 'Real-time chat with WebSocket and Pub/Sub',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-[#0f0f0f]">{children}</body>
    </html>
  )
}
