import { useState, type ReactNode } from 'react';
import { Button } from './ui/button';
import { Avatar, AvatarFallback } from './ui/avatar';
import { Sheet, SheetContent, SheetDescription, SheetTitle, SheetTrigger } from './ui/sheet';
import { Home, BookOpen, KeyRound, Network, GraduationCap, Sparkles, Bell, Search, Menu, Moon, Sun, Command } from 'lucide-react';
import type { AccessRequest } from '../api/onboarding';

const menuItems = [
  { id: 'dashboard', label: 'Главная', icon: Home },
  { id: 'knowledge', label: 'База знаний', icon: BookOpen },
  { id: 'access', label: 'Доступы', icon: KeyRound },
  { id: 'projects', label: 'Проект и инструменты', icon: Network },
  { id: 'internship', label: 'Моя стажировка', icon: GraduationCap },
  { id: 'ai-assistant', label: 'AI-buddy', icon: Sparkles },
];

export function DashboardLayout({ activeTab, setActiveTab, isDarkMode, toggleDarkMode, onSearch, access, children }: {
  activeTab: string; setActiveTab: (tab: string) => void; isDarkMode: boolean; toggleDarkMode: () => void;
  onSearch: (query: string) => void; access: AccessRequest | null; children: ReactNode;
}) {
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false);
  const [topQuery, setTopQuery] = useState('');
  const [notificationsOpen, setNotificationsOpen] = useState(false);
  const current = menuItems.find(item => item.id === activeTab);

  return <div className="studio-shell">
    <aside className="studio-sidebar" aria-label="Основная навигация">
      <button className="studio-brand" onClick={() => setActiveTab('dashboard')} aria-label="AI-buddy Workspace · главная" title="AI-buddy Workspace"><Command size={27} strokeWidth={3} /></button>
      <nav className="studio-nav">{menuItems.map(item => { const Icon = item.icon; return <button key={item.id} className={`studio-nav-button ${activeTab === item.id ? 'is-active' : ''}`} onClick={() => setActiveTab(item.id)} aria-label={item.label} aria-current={activeTab === item.id ? 'page' : undefined} title={item.label} data-label={item.label}><Icon size={21} strokeWidth={2} /><span className="sr-only">{item.label}</span>{item.id === 'access' && access?.status === 'На согласовании' && <i className="studio-notification-dot" />}</button>; })}</nav>
      <div className="studio-sidebar-bottom"><button className="studio-profile-button" onClick={() => setActiveTab('internship')} aria-label="Моя стажировка" title="Моя стажировка">Я</button></div>
    </aside>
    <div className="studio-main"><header className="studio-topbar"><div className="studio-top-left"><Sheet open={isMobileMenuOpen} onOpenChange={setIsMobileMenuOpen}><SheetTrigger asChild><Button variant="ghost" size="icon" className="lg:hidden" aria-label="Открыть меню"><Menu className="w-5 h-5" /></Button></SheetTrigger><SheetContent side="left" className="studio-mobile-sheet w-72 p-6"><SheetTitle className="studio-mobile-brand"><Command size={23} /> AI-buddy Workspace</SheetTitle><SheetDescription className="sr-only">Разделы рабочего пространства</SheetDescription><nav className="studio-mobile-nav">{menuItems.map(item => { const Icon = item.icon; return <button key={item.id} onClick={() => { setActiveTab(item.id); setIsMobileMenuOpen(false); }} className={activeTab === item.id ? 'is-active' : ''}><Icon size={19} />{item.label}</button>; })}</nav></SheetContent></Sheet><span className="studio-breadcrumb">AI-buddy <span>/</span></span><h1>{current?.label}</h1></div>
      <div className="studio-top-actions"><form className="studio-top-search" onSubmit={event => { event.preventDefault(); onSearch(topQuery); setTopQuery(''); }}><Search size={18} /><input value={topQuery} onChange={event => setTopQuery(event.target.value)} placeholder="Найти документ" aria-label="Найти документ" /></form><button className="studio-theme-button" onClick={toggleDarkMode} aria-label={isDarkMode ? 'Светлая тема' : 'Тёмная тема'} title={isDarkMode ? 'Светлая тема' : 'Тёмная тема'}>{isDarkMode ? <Sun size={18} /> : <Moon size={18} />}</button><div className="studio-notice-wrap"><button className="studio-theme-button" onClick={() => setNotificationsOpen(value => !value)} aria-label="Уведомления" aria-expanded={notificationsOpen}><Bell size={18} />{access?.status === 'На согласовании' && <i />}</button>{notificationsOpen && <div className="studio-popover" role="status">{access ? [access.externalId ? `Заявка ${access.externalId}` : null, access.status].filter(Boolean).join(': ') : 'Новых уведомлений нет'}</div>}</div><Avatar className="studio-avatar"><AvatarFallback>Я</AvatarFallback></Avatar></div></header>
      <main className="studio-content" id="main-content">{children}</main></div>
  </div>;
}
