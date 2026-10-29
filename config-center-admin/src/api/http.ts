import axios from 'axios'

export const api = axios.create({
  baseURL: '/api/config/v1',
  timeout: 10000
})
