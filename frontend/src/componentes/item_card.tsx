import { useState } from 'react'
import './item_card.css'

function Card_item() {
  const [count, setCount] = useState(0)

  return (
    <>
    <div className='no_card'>
        <section className='no_conteudo'>
            <div className='no_properties'>
                <h3>Nó teste</h3>
                <p>IP: 128.990.345.089</p>
            
            </div>

            <div className='no_actions'>
                <button className="print_log" > Histórico </button>
                <button className='print_kvalue'>Exibir K-Value</button>
            </div>

    </section>
        <section className='no_change_kvalue'>
            <div className='input_kvalue'>
                <label>KEY: </label>
                <input type= 'text'  className="input"/>
                <label>VALUE: </label>
                <input type= 'text'  className="input"/>
            </div>
            <div className='apply_changes'>
                <button className="set_kvalue" > SET </button>
                <button className='delete_kvalue'>DELETE</button>
                
            </div>
            

        </section>
    </div>  
        



      
        
    </>
  )
}

export default Card_item;