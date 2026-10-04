import { createApp } from 'vue'
import App from './App.vue'

// Restore the theme before mounting to avoid a flash of the wrong palette.
document.documentElement.dataset.theme = localStorage.getItem('msgui.theme') ?? 'dark'

createApp(App).mount('#app')
