// 浏览器 Web Crypto 不提供 MD5；仅用于兼容游戏侧现有签名协议。
export function md5Upper(text: string): string {
  const input = new TextEncoder().encode(text)
  const size = Math.ceil((input.length + 9) / 64) * 64
  const bytes = new Uint8Array(size)
  bytes.set(input)
  bytes[input.length] = 0x80
  const view = new DataView(bytes.buffer)
  view.setUint32(size - 8, (input.length * 8) >>> 0, true)
  view.setUint32(size - 4, Math.floor(input.length / 0x20000000) >>> 0, true)

  const shifts = [
    7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22,
    5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20,
    4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23,
    6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21
  ]
  const constants = Array.from({ length: 64 }, (_, index) =>
    Math.floor(Math.abs(Math.sin(index + 1)) * 0x100000000) >>> 0)
  const rotate = (value: number, shift: number) => (value << shift) | (value >>> (32 - shift))
  const state = [0x67452301, 0xefcdab89, 0x98badcfe, 0x10325476]

  for (let offset = 0; offset < size; offset += 64) {
    let [a, b, c, d] = state
    for (let i = 0; i < 64; i++) {
      let f: number
      let g: number
      if (i < 16) { f = (b & c) | (~b & d); g = i }
      else if (i < 32) { f = (d & b) | (~d & c); g = (5 * i + 1) % 16 }
      else if (i < 48) { f = b ^ c ^ d; g = (3 * i + 5) % 16 }
      else { f = c ^ (b | ~d); g = (7 * i) % 16 }
      const word = view.getUint32(offset + 4 * g, true)
      const next = (b + rotate((a + f + constants[i] + word) >>> 0, shifts[i])) >>> 0
      a = d; d = c; c = b; b = next
    }
    state[0] = (state[0] + a) >>> 0
    state[1] = (state[1] + b) >>> 0
    state[2] = (state[2] + c) >>> 0
    state[3] = (state[3] + d) >>> 0
  }

  const output = new Uint8Array(16)
  const result = new DataView(output.buffer)
  state.forEach((value, i) => result.setUint32(i * 4, value, true))
  return Array.from(output, byte => byte.toString(16).padStart(2, '0')).join('').toUpperCase()
}
