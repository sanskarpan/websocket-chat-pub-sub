'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { 
  MessageSquare, Users, Settings, Send, Paperclip, 
  Smile, MoreVertical, Search, Plus, LogIn, LogOut,
  Wifi, WifiOff, ChevronLeft, Hash, Lock, AtSign
} from 'lucide-react'
import { format } from 'date-fns'

const WS_URL = 'ws://localhost:8086/ws'
const API_URL = 'http://localhost:8085'

interface User {
  id: string
  username: string
  display_name: string
  avatar_url?: string
  status: 'online' | 'away' | 'dnd' | 'offline'
}

interface Room {
  id: string
  name: string
  type: 'direct' | 'group' | 'channel'
  member_count: number
  avatar_url?: string
}

interface Message {
  id: string
  room_id: string
  user_id: string
  content: string
  content_type: string
  created_at: string
  user?: User
}

interface ServerMessage {
  id: string
  type: string
  timestamp: string
  data: any
}

export default function ChatApp() {
  const [isConnected, setIsConnected] = useState(false)
  const [showLogin, setShowLogin] = useState(true)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [email, setEmail] = useState('')
  const [isRegistering, setIsRegistering] = useState(false)
  const [token, setToken] = useState('')
  
  const [rooms, setRooms] = useState<Room[]>([])
  const [selectedRoom, setSelectedRoom] = useState<Room | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [newMessage, setNewMessage] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [showSidebar, setShowSidebar] = useState(true)
  
  const wsRef = useRef<WebSocket | null>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [])

  useEffect(() => {
    scrollToBottom()
  }, [messages, scrollToBottom])

  useEffect(() => {
    const storedToken = localStorage.getItem('token')
    const storedRefresh = localStorage.getItem('refresh_token')
    if (storedToken && storedRefresh) {
      setToken(storedToken)
      setShowLogin(false)
      connect(storedToken)
      fetchRooms(storedToken)
    }
  }, [])

  const connect = useCallback((authToken: string) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.close()
    }

    const ws = new WebSocket(`${WS_URL}?token=${authToken}`)
    wsRef.current = ws

    ws.onopen = () => {
      setIsConnected(true)
      console.log('Connected to WebSocket')
      
      ws.send(JSON.stringify({
        id: crypto.randomUUID(),
        type: 'subscribe',
        timestamp: new Date().toISOString(),
        data: {
          room_ids: rooms.map(r => r.id)
        }
      }))
    }

    ws.onmessage = (event) => {
      try {
        const msg: ServerMessage = JSON.parse(event.data)
        handleServerMessage(msg)
      } catch (e) {
        console.error('Failed to parse message:', e)
      }
    }

    ws.onclose = () => {
      setIsConnected(false)
      console.log('Disconnected from WebSocket')
    }

    ws.onerror = (error) => {
      console.error('WebSocket error:', error)
    }
  }, [rooms])

  const handleServerMessage = (msg: ServerMessage) => {
    switch (msg.type) {
      case 'connection':
        console.log('Connection established:', msg.data)
        break
      case 'ack':
        console.log('Message acknowledged:', msg.data)
        break
      case 'new_message':
        const newMsg: Message = msg.data.message
        setMessages(prev => [...prev, newMsg])
        break
      case 'typing':
        break
      case 'presence':
        break
    }
  }

  const sendMessage = () => {
    if (!newMessage.trim() || !wsRef.current || !selectedRoom) return

    const msg = {
      id: crypto.randomUUID(),
      type: 'message',
      timestamp: new Date().toISOString(),
      data: {
        room_id: selectedRoom.id,
        content: newMessage
      }
    }

    wsRef.current.send(JSON.stringify(msg))
    setNewMessage('')
  }

  const handleLogin = async () => {
    try {
      const res = await fetch(`${API_URL}/api/v1/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password })
      })

      if (res.ok) {
        const data = await res.json()
        setToken(data.access_token)
        localStorage.setItem('token', data.access_token)
        localStorage.setItem('refresh_token', data.refresh_token)
        connect(data.access_token)
        setShowLogin(false)
        fetchRooms(data.access_token)
      } else {
        const error = await res.json().catch(() => ({ message: 'Login failed' }))
        alert(`Login failed: ${error.message || 'Invalid credentials'}`)
      }
    } catch (e) {
      console.error('Login error:', e)
      alert('Unable to reach the server. Please try again later.')
    }
  }

  const handleRegister = async () => {
    try {
      const res = await fetch(`${API_URL}/api/v1/auth/register`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username,
          email,
          password,
          display_name: username
        })
      })

      if (res.ok) {
        setIsRegistering(false)
        alert('Registration successful! Please login.')
      } else {
        const error = await res.json().catch(() => ({ message: 'Registration failed' }))
        alert(`Registration failed: ${error.message || 'Unknown error'}`)
      }
    } catch (e) {
      console.error('Register error:', e)
      alert('Unable to reach the server. Please try again later.')
    }
  }

  const fetchRooms = async (authToken: string) => {
    try {
      const res = await fetch(`${API_URL}/api/v1/rooms`, {
        headers: { 'Authorization': `Bearer ${authToken}` }
      })
      if (res.ok) {
        const data = await res.json()
        setRooms(data)
      } else {
        console.error('Failed to fetch rooms, status:', res.status)
        setRooms([])
      }
    } catch (e) {
      console.error('Failed to fetch rooms:', e)
      setRooms([])
    }
  }

  const handleLogout = () => {
    wsRef.current?.close()
    setToken('')
    setIsConnected(false)
    setShowLogin(true)
    setRooms([])
    setMessages([])
    setSelectedRoom(null)
    localStorage.removeItem('token')
    localStorage.removeItem('refresh_token')
  }

  if (showLogin) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-[#0f0f0f] p-4">
        <motion.div 
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          className="w-full max-w-md"
        >
          <div className="glass rounded-2xl p-8">
            <div className="flex items-center justify-center mb-8">
              <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-violet-500 to-fuchsia-500 flex items-center justify-center">
                <MessageSquare className="w-8 h-8 text-white" />
              </div>
            </div>
            
            <h1 className="text-2xl font-bold text-center mb-2">Welcome Back</h1>
            <p className="text-gray-400 text-center mb-8">Sign in to continue to your chats</p>
            
            {isRegistering ? (
              <div className="space-y-4">
                <input
                  type="text"
                  placeholder="Username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  className="w-full px-4 py-3 rounded-xl bg-[#1a1a1a] border border-[#262626] focus:border-violet-500 focus:outline-none transition-colors"
                />
                <input
                  type="email"
                  placeholder="Email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="w-full px-4 py-3 rounded-xl bg-[#1a1a1a] border border-[#262626] focus:border-violet-500 focus:outline-none transition-colors"
                />
                <input
                  type="password"
                  placeholder="Password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="w-full px-4 py-3 rounded-xl bg-[#1a1a1a] border border-[#262626] focus:border-violet-500 focus:outline-none transition-colors"
                />
                <button
                  onClick={handleRegister}
                  className="w-full py-3 rounded-xl bg-gradient-to-r from-violet-500 to-fuchsia-500 text-white font-semibold hover:opacity-90 transition-opacity"
                >
                  Create Account
                </button>
                <p className="text-center text-gray-400">
                  Already have an account?{' '}
                  <button onClick={() => setIsRegistering(false)} className="text-violet-400 hover:underline">
                    Sign in
                  </button>
                </p>
              </div>
            ) : (
              <div className="space-y-4">
                <input
                  type="email"
                  placeholder="Email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="w-full px-4 py-3 rounded-xl bg-[#1a1a1a] border border-[#262626] focus:border-violet-500 focus:outline-none transition-colors"
                />
                <input
                  type="password"
                  placeholder="Password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="w-full px-4 py-3 rounded-xl bg-[#1a1a1a] border border-[#262626] focus:border-violet-500 focus:outline-none transition-colors"
                />
                <button
                  onClick={handleLogin}
                  className="w-full py-3 rounded-xl bg-gradient-to-r from-violet-500 to-fuchsia-500 text-white font-semibold hover:opacity-90 transition-opacity"
                >
                  Sign In
                </button>
                <p className="text-center text-gray-400">
                  Don&apos;t have an account?{' '}
                  <button onClick={() => setIsRegistering(true)} className="text-violet-400 hover:underline">
                    Create one
                  </button>
                </p>
              </div>
            )}
          </div>
        </motion.div>
      </div>
    )
  }

  return (
    <div className="h-screen flex bg-[#0f0f0f]">
      {/* Sidebar */}
      <AnimatePresence>
        {showSidebar && (
          <motion.aside
            initial={{ width: 0, opacity: 0 }}
            animate={{ width: 280, opacity: 1 }}
            exit={{ width: 0, opacity: 0 }}
            className="border-r border-[#262626] flex flex-col"
          >
            {/* Workspace Header */}
            <div className="p-4 border-b border-[#262626]">
              <div className="flex items-center justify-between mb-4">
                <h1 className="font-bold text-lg flex items-center gap-2">
                  <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-violet-500 to-fuchsia-500 flex items-center justify-center">
                    <MessageSquare className="w-4 h-4 text-white" />
                  </div>
                  Chat
                </h1>
                <button 
                  onClick={handleLogout}
                  className="p-2 rounded-lg hover:bg-[#1a1a1a] text-gray-400 hover:text-white transition-colors"
                >
                  <LogOut className="w-5 h-5" />
                </button>
              </div>
              
              {/* Search */}
              <div className="relative">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
                <input
                  type="text"
                  placeholder="Search..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="w-full pl-10 pr-4 py-2 rounded-xl bg-[#1a1a1a] border border-[#262626] focus:border-violet-500 focus:outline-none text-sm"
                />
              </div>
            </div>

            {/* Channels */}
            <div className="flex-1 overflow-y-auto p-2">
              <div className="px-2 py-1 flex items-center justify-between text-xs font-semibold text-gray-400 uppercase tracking-wider">
                <span>Channels</span>
                <Plus className="w-4 h-4 cursor-pointer hover:text-white" />
              </div>
              {rooms.filter(r => r.type === 'channel').map(room => (
                <button
                  key={room.id}
                  onClick={() => setSelectedRoom(room)}
                  className={`w-full px-3 py-2 rounded-lg flex items-center gap-2 transition-colors ${
                    selectedRoom?.id === room.id 
                      ? 'bg-violet-500/20 text-violet-400' 
                      : 'hover:bg-[#1a1a1a] text-gray-300'
                  }`}
                >
                  <Hash className="w-4 h-4" />
                  <span className="truncate">{room.name}</span>
                </button>
              ))}

              <div className="px-2 py-1 mt-4 flex items-center justify-between text-xs font-semibold text-gray-400 uppercase tracking-wider">
                <span>Direct Messages</span>
                <Plus className="w-4 h-4 cursor-pointer hover:text-white" />
              </div>
              {rooms.filter(r => r.type === 'direct').map(room => (
                <button
                  key={room.id}
                  onClick={() => setSelectedRoom(room)}
                  className={`w-full px-3 py-2 rounded-lg flex items-center gap-2 transition-colors ${
                    selectedRoom?.id === room.id 
                      ? 'bg-violet-500/20 text-violet-400' 
                      : 'hover:bg-[#1a1a1a] text-gray-300'
                  }`}
                >
                  <div className="w-6 h-6 rounded-full bg-gradient-to-br from-emerald-400 to-cyan-400 flex items-center justify-center text-xs font-bold">
                    {room.name[0]}
                  </div>
                  <span className="truncate">{room.name}</span>
                  <div className="w-2 h-2 rounded-full bg-emerald-500 ml-auto" />
                </button>
              ))}
            </div>

            {/* User Info */}
            <div className="p-4 border-t border-[#262626]">
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 rounded-full bg-gradient-to-br from-violet-500 to-fuchsia-500 flex items-center justify-center font-bold">
                  {username ? username[0].toUpperCase() : 'U'}
                </div>
                <div className="flex-1 min-w-0">
                  <p className="font-medium truncate">{username || 'User'}</p>
                  <p className="text-xs text-gray-400 flex items-center gap-1">
                    <span className={`w-2 h-2 rounded-full ${isConnected ? 'bg-emerald-500' : 'bg-red-500'}`} />
                    {isConnected ? 'Online' : 'Offline'}
                  </p>
                </div>
              </div>
            </div>
          </motion.aside>
        )}
      </AnimatePresence>

      {/* Main Chat Area */}
      <main className="flex-1 flex flex-col min-w-0">
        {/* Header */}
        <header className="h-16 border-b border-[#262626] flex items-center justify-between px-4">
          <div className="flex items-center gap-3">
            <button 
              onClick={() => setShowSidebar(!showSidebar)}
              className={`p-2 rounded-lg hover:bg-[#1a1a1a] transition-colors ${!showSidebar ? '' : ''}`}
            >
              <ChevronLeft className={`w-5 h-5 transition-transform ${!showSidebar ? 'rotate-180' : ''}`} />
            </button>
            {selectedRoom && (
              <div className="flex items-center gap-2">
                {selectedRoom.type === 'channel' ? (
                  <Hash className="w-5 h-5 text-gray-400" />
                ) : (
                  <AtSign className="w-5 h-5 text-gray-400" />
                )}
                <h2 className="font-semibold">{selectedRoom.name}</h2>
                <span className="text-gray-400 text-sm">#{selectedRoom.id}</span>
              </div>
            )}
          </div>
          
          <div className="flex items-center gap-2">
            <button className="p-2 rounded-lg hover:bg-[#1a1a1a] transition-colors">
              <Users className="w-5 h-5 text-gray-400" />
            </button>
            <button className="p-2 rounded-lg hover:bg-[#1a1a1a] transition-colors">
              <Settings className="w-5 h-5 text-gray-400" />
            </button>
          </div>
        </header>

        {/* Messages */}
        <div className="flex-1 overflow-y-auto p-4 space-y-4">
          {messages.map((msg, i) => (
            <motion.div
              key={msg.id}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.05 }}
              className="flex gap-3 group"
            >
              <div className="w-10 h-10 rounded-full bg-gradient-to-br from-violet-500 to-fuchsia-500 flex-shrink-0 flex items-center justify-center font-bold">
                {msg.user?.display_name?.[0] || 'U'}
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-baseline gap-2">
                  <span className="font-semibold">{msg.user?.display_name || 'User'}</span>
                  <span className="text-xs text-gray-400">
                    {format(new Date(msg.created_at), 'h:mm a')}
                  </span>
                </div>
                <p className="text-gray-300 break-words">{msg.content}</p>
              </div>
              <button className="opacity-0 group-hover:opacity-100 p-1 rounded hover:bg-[#1a1a1a] transition-all">
                <MoreVertical className="w-4 h-4 text-gray-400" />
              </button>
            </motion.div>
          ))}
          <div ref={messagesEndRef} />
        </div>

        {/* Input Area */}
        <div className="p-4 border-t border-[#262626]">
          <div className="flex items-end gap-2">
            <button className="p-2 rounded-lg hover:bg-[#1a1a1a] transition-colors">
              <Plus className="w-5 h-5 text-gray-400" />
            </button>
            <div className="flex-1 relative">
              <input
                type="text"
                placeholder={`Message #${selectedRoom?.name || 'channel'}`}
                value={newMessage}
                onChange={(e) => setNewMessage(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && sendMessage()}
                className="w-full px-4 py-3 rounded-xl bg-[#1a1a1a] border border-[#262626] focus:border-violet-500 focus:outline-none"
              />
              <div className="absolute right-3 top-1/2 -translate-y-1/2 flex items-center gap-1">
                <button className="p-1 rounded hover:bg-[#262626] transition-colors">
                  <Smile className="w-5 h-5 text-gray-400" />
                </button>
                <button className="p-1 rounded hover:bg-[#262626] transition-colors">
                  <Paperclip className="w-5 h-5 text-gray-400" />
                </button>
              </div>
            </div>
            <button 
              onClick={sendMessage}
              disabled={!newMessage.trim()}
              className="p-3 rounded-xl bg-violet-500 text-white disabled:opacity-50 disabled:cursor-not-allowed hover:bg-violet-600 transition-colors"
            >
              <Send className="w-5 h-5" />
            </button>
          </div>
        </div>
      </main>
    </div>
  )
}
