import './item_card.css'
import type { NodeInfo } from '../api/types';

function Card_item({node}:{node:NodeInfo}) {

  return (
    <>
    <div className={'no_card '+(node.is_leader?'isleader':'')}>
        <section className='no_conteudo'>
            <div className='no_properties'>
                <h3>{node.name}</h3>
                <p>{node.address}</p>
            
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