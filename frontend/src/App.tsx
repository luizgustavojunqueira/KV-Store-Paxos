import { useState } from 'react'
import './App.css'
import './componentes/item_card'
import Card_item from './componentes/item_card'

function App() {
  const [count, setCount] = useState(0)

  return (
    <>
      <Card_item></Card_item>
      <Card_item></Card_item>
    </>
  )
}

export default App
