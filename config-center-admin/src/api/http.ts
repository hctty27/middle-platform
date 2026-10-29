import axios from 'axios'

export const api = axios.create({
  baseURL: '/api/config',
  timeout: 10000
})
